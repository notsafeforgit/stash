"""UTF-8 filename budgets for gallery-dl, preserving IDs and attachment numbers.

Only rendered title fields may shrink. Source metadata is never modified.
Existing per-field caps remain compatibility ceilings; the complete component
and all configured temporary forms must also fit the filesystem's byte limit.
"""
import hashlib
import json
import os
from pathlib import Path
import string
import types

from gallery_dl import config, exception, formatter, util


def size(value):
    return len(value.encode('utf-8'))


def clipped(value, budget):
    if size(value) <= budget:
        return value
    marker = '...' if budget >= 3 else ''
    return value.encode('utf-8')[:budget-size(marker)].decode('utf-8', 'ignore') + marker


def name_max(directory):
    path = Path(directory)
    while True:
        try:
            return os.pathconf(path, 'PC_NAME_MAX')
        except FileNotFoundError:
            if path == path.parent:
                raise
            path = path.parent


def ytdl_item(metadata):
    return (str(metadata.get('_url', '')).startswith('ytdl:')
            or '_ytdl_info_dict' in metadata or str(metadata.get('category', '')).startswith('ytdl'))


class Template:
    def __init__(self, source, default):
        self.parts = []
        self.flexible = []
        self.has_identity = False
        if not isinstance(source, str) or source.startswith('\f'):
            return
        for literal, field, spec, conversion in string.Formatter().parse(source):
            if literal:
                self.parts.append((None, literal))
            if field is None:
                continue
            expression = '{' + field
            if conversion:
                expression += '!' + conversion
            if spec:
                expression += ':' + spec
            expression += '}'
            if field.split('[', 1)[0] in {'title', 'fulltitle'} or field == '_reddit[title]':
                self.flexible.append(len(self.parts))
            if field.split('[', 1)[0] in {'id', 'post_id', 'tweet_id', 'media_id', 'file_id', 'filename'} or field == '_reddit[id]':
                self.has_identity = True
            self.parts.append((formatter.parse(expression, default).format_map, None))

    def render(self, values):
        return [function(values) if function else literal for function, literal in self.parts]


class Budget:
    def __init__(self, job):
        self.job = job
        self.pathfmt = pathfmt = job.pathfmt
        self.original = pathfmt.build_filename
        self.info = None
        self.playlist_index = None
        self.limit = None
        self.directory = None
        configured = job.extractor.config('filename')
        default = job.extractor.config('keywords-default')
        if default is None:
            default = util.NONE
        default_format = job.extractor.filename_fmt if configured is None else configured
        self.templates = {}
        if isinstance(configured, dict):
            default_format = configured.get('', job.extractor.filename_fmt)
            formats = [value for condition, value in configured.items() if condition]
            for (_, function), source in zip(pathfmt.filename_conditions, formats):
                self.templates[function] = Template(source, default)
        self.templates[pathfmt.filename_formatter] = Template(default_format, default)
        named = config.getg('postprocessor') or {}
        self.gif = self.exif = False
        self.metadata_extensions = []
        for processor in job.extractor.config_accumulate('postprocessors') or ():
            options = named.get(processor, {}) if isinstance(processor, str) else processor
            command = options.get('command', ())
            words = command.split() if isinstance(command, str) else command
            self.gif |= any(str(word).endswith('gif_to_av1_qsv.py') for word in words)
            self.exif |= any(Path(str(word)).name == 'exiftool' for word in words)
            if options.get('name') == 'metadata' and options.get('mode') not in {'print','modify','delete','jsonl'} and not options.get('filename'):
                extension = options.get('extension-format') or options.get('extension') or ('txt' if options.get('mode') in {'custom','tags'} else 'json')
                self.metadata_extensions.append(formatter.parse(extension, default).format_map)

    def begin(self, metadata):
        self.info = metadata.get('_ytdl_info_dict')
        self.playlist_index = None

    def clean(self, parts):
        return self.pathfmt.clean_path(self.pathfmt.clean_segment(''.join(parts)))

    def _variants(self, template, values, replacements):
        def render(fields):
            parts = template.render(fields)
            for index, replacement in replacements.items():
                parts[index] = replacement
            return self.clean(parts)
        full = render(values)
        forms = [full, full + '.part']
        forms.extend(render(dict(values, extension=extension(values))) for extension in self.metadata_extensions)
        if self.gif and str(self.pathfmt.extension).lower() == 'gif':
            converted = str(Path(full).with_suffix('.mkv'))
            # tempfile.mkstemp: leading dot, separator, eight random characters.
            forms += [converted, '.' + converted + '.12345678.part']
        if self.exif:
            forms.append(full + '_exiftool_tmp')
        if self.info is not None:
            info = self.info
            # gallery-dl builds yt-dlp's template with extension temporarily
            # blank, then appends %(ext)s. Account for formats using {ext} too.
            prefix = render(dict(values, extension=self.pathfmt.prefix))
            if self.playlist_index is not None:
                prefix += self.playlist_index + '.'
            ext = str(info.get('ext') or self.pathfmt.extension or 'mp4')
            main = prefix + ext
            downloads = [(main, info)]
            if info.get('requested_formats'):
                # yt-dlp correct_ext only replaces a suffix matching info.ext.
                # A gallery template using {ext} instead of {extension} leaves
                # that field in prefix and therefore adds a second extension.
                stem, dot, suffix = main.rpartition('.')
                if not dot or suffix != ext:
                    stem = main
                downloads.append((stem + '.' + ext, info))
                for item in info['requested_formats']:
                    for format_ext in {ext, str(item.get('ext') or ext)}:
                        downloads.append((stem + '.f' + str(item.get('format_id', '')) + '.' + format_ext, item))
            for base, item in downloads:
                forms += [base + '.part', base + '.ytdl', base + '.temp']
                if info.get('section_start') is not None or info.get('section_end') is not None:
                    forms.append(base + '.keyframes.temp')
                protocol = str(item.get('protocol') or info.get('protocol') or '')
                if (item.get('fragments') or item.get('fragment_count') or item.get('is_live') or info.get('is_live')
                        or any(kind in protocol for kind in ('m3u8', 'dash', 'ism', 'f4m'))):
                    digits = max(20, len(str(item.get('fragment_count') or 0)))
                    forms.append(base + '.part-Frag' + '9' * digits + '.part')
        return forms

    def _selected(self, values):
        pf = self.pathfmt
        if pf.filename_conditions:
            for condition, function in pf.filename_conditions:
                if condition(values):
                    return function
        return pf.filename_formatter

    def build(self, metadata):
        try:
            pf = self.pathfmt
            if self.directory != pf.realdirectory:
                self.directory = pf.realdirectory
                self.limit = min(255, name_max(self.directory))
            values = metadata
            # A temporary extensionless rendering must use the same title
            # budget as the final filename, so resume/finalize agree on its stem.
            if pf.extension and metadata.get('extension') != pf.prefix + pf.extension:
                values = dict(metadata, extension=pf.prefix + pf.extension)
            function = self._selected(values)
            template = self.templates[function]
            raw = self.original(metadata)
            if not template.parts:
                if size(raw) + 35 <= self.limit:
                    return raw
                raise ValueError('Oversized custom filename has no identifiable title field')
            parts = template.render(values)
            replacements = {index: parts[index] for index in template.flexible}
            longest = max(map(size, self._variants(template, values, replacements)))
            if longest <= self.limit:
                return raw
            # Old archived media keeps its original pathname; skip hooks do not
            # create download/transform temporaries for a completed archive item.
            if size(raw) <= self.limit and self.job.archive is not None and self.job.archive.check(metadata):
                old = Path(pf.realdirectory) / raw
                if old.is_file() or (self.gif and old.suffix.lower() == '.gif' and old.with_suffix('.mkv').is_file()):
                    return raw
            blank = dict.fromkeys(template.flexible, '')
            budget = self.limit - max(map(size, self._variants(template, values, blank)))
            suffix = ''
            if not template.has_identity:
                identity = {key: metadata[key] for key in ('category', 'id', 'post_id', 'tweet_id', 'media_id', 'file_id')
                            if isinstance(metadata.get(key), (str, int))}
                if not any(key != 'category' for key in identity):
                    identity.update({key: metadata[key] for key in ('title','fulltitle','webpage_url','original_url')
                                     if isinstance(metadata.get(key), str)})
                suffix = '~' + hashlib.sha256(json.dumps(identity, sort_keys=True, ensure_ascii=False).encode()).hexdigest()[:16]
                budget -= size(suffix)
            if not replacements or budget < 0:
                raise ValueError('Filename identifiers, item number, extension and temporary suffix exceed the byte limit')
            for offset, index in enumerate(template.flexible):
                allowance = budget // (len(template.flexible) - offset)
                replacements[index] = clipped(parts[index], allowance)
                budget -= size(replacements[index])
            replacements[template.flexible[-1]] += suffix
            if max(map(size, self._variants(template, values, replacements))) > self.limit:
                raise ValueError('Unable to fit filename without shortening identifiers')
            output = template.render(metadata)
            for index, value in replacements.items():
                output[index] = value
            result = self.clean(output)
            # Never silently abandon an interrupted download under an old name.
            # Normal resumable paths already fit and are returned above.
            if raw != result and size(raw) <= self.limit:
                old = Path(pf.realdirectory) / raw
                old_media = [old]
                if self.gif and old.suffix.lower() == '.gif':
                    old_media.append(old.with_suffix('.mkv'))
                if any(path.is_file() for path in old_media):
                    raise ValueError('Existing unarchived media uses the old oversized filename; retain it for explicit migration')
                if size(raw) + 5 <= self.limit and Path(str(old) + '.part').is_file():
                    raise ValueError('An existing partial uses the old oversized filename; retain it for explicit migration')
            return result
        except exception.FilenameFormatError:
            raise
        except (ValueError, OSError) as error:
            raise exception.FilenameFormatError(error) from error


def install(job):
    existing = getattr(job, '_stash_filename_budget', None)
    if existing is not None:
        return existing
    budget = job._stash_filename_budget = Budget(job)
    job.pathfmt.build_filename = budget.build
    original_get = job.get_downloader
    def get_downloader(self, scheme):
        downloader = original_get(scheme)
        if scheme == 'ytdl' and downloader is not None and not getattr(downloader, '_stash_filename_budget', False):
            downloader._stash_filename_budget = True
            original_video = downloader._download_video
            def video(self, ytdl, pathfmt, info):
                budget.info = info
                if self.outtmpl:
                    raise exception.FilenameFormatError(ValueError('Custom yt-dlp outtmpl bypasses the native filename budget'))
                return original_video(ytdl, pathfmt, info)
            downloader._download_video = types.MethodType(video, downloader)
            original_playlist = downloader._download_playlist
            def playlist(self, ytdl, pathfmt, info):
                # Some non-ytdl extractors hand the downloader a lazy playlist.
                # Budget each resolved entry without consuming future entries
                # (and their API calls) ahead of the installed downloader.
                def entries():
                    for entry in info['entries']:
                        if entry:
                            budget.info = entry
                            # Use the downloader's own padding/NA rules.
                            budget.playlist_index = ytdl.evaluate_outtmpl('%(playlist_index)s', entry)
                            filename = pathfmt.build_filename(dict(pathfmt.kwdict, extension=pathfmt.prefix))
                            template = (pathfmt.realdirectory + filename).replace('%', '%%')
                            self._set_outtmpl(ytdl, template + '%(playlist_index)s.%(ext)s')
                        yield entry
                try:
                    return original_playlist(ytdl, pathfmt, dict(info, entries=entries()))
                finally:
                    budget.playlist_index = None
            downloader._download_playlist = types.MethodType(playlist, downloader)
        return downloader
    job.get_downloader = types.MethodType(get_downloader, job)
    return budget
