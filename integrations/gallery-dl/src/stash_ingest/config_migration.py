"""Convert local gallery-dl JSON layers without activating or rewriting them."""

import argparse
import copy
from dataclasses import dataclass
import os
from pathlib import Path
import re
import sys
import tempfile

from .configuration import (ASSET_LIMIT, SCHEMA, Configuration, _access_option, _asset,
                            _json_file, _path, private_arguments, profile_bytes)
from .encoding import InvalidData, digest, encode


def pointer(parts):
    return "".join("/" + str(part).replace("~", "~0").replace("/", "~1") for part in parts)


@dataclass
class Setting:
    value: object
    sources: list

    @classmethod
    def read(cls, value, filename, position=()):
        sources = [{"file": str(filename), "pointer": pointer(position)}]
        if isinstance(value, dict):
            value = {key: cls.read(child, filename, (*position, key)) for key, child in value.items()}
        elif isinstance(value, list):
            value = [cls.read(child, filename, (*position, index)) for index, child in enumerate(value)]
        return cls(value, sources)

    def plain(self):
        if isinstance(self.value, dict):
            return {key: child.plain() for key, child in self.value.items()}
        if isinstance(self.value, list):
            return [child.plain() for child in self.value]
        return self.value

    def merge(self, incoming):
        if isinstance(self.value, dict) and isinstance(incoming.value, dict):
            merged = dict(self.value)
            for key, child in incoming.value.items():
                merged[key] = merged[key].merge(child) if key in merged else child
            return Setting(merged, self.sources + incoming.sources)
        return incoming


def legacy_processor(options):
    if not isinstance(options, dict):
        return False
    spec = options.get("function", "")
    module, _, function = spec.rpartition(":") if isinstance(spec, str) else ("", "", "")
    if Path(module).name != "gallery_catalog_hook.py":
        return False
    if options.get("name") != "python" or function not in {"prepare", "complete"}:
        raise InvalidData("An unknown legacy catalog processor requires explicit conversion")
    return True


class Converter:
    """Preserve effective JSON order and private-value provenance across layers."""

    def __init__(self, filenames, root, locks, *, working_directory=None, assets=(), category=None):
        if not 1 <= len(filenames) <= 16:
            raise InvalidData("Choose one to sixteen ordered gallery-dl JSON files")
        self.working = Path(working_directory or os.getcwd()).resolve(strict=True)
        self.root_uuid, self.root = Configuration._root(root, self.working, media=True)
        _, self.locks = Configuration._root(locks, self.working, media=False)
        self.root_definition = {"uuid": self.root_uuid, "path": str(self.root.path), "identity": list(self.root.identity)}
        self.lock_definition = {"path": str(self.locks.path), "identity": list(self.locks.identity)}
        self.explicit_assets = {_path(str(path), self.working) for path in assets}
        self.bindings = {}
        self.removed = []
        self.category, self.excluded = category, []
        merged = None
        for filename in filenames:
            path = Path(filename).resolve(strict=True)
            value = _json_file(path)
            if not isinstance(value, dict):
                raise InvalidData("Gallery-dl configuration must be a JSON object")
            incoming = Setting.read(value, path)
            merged = merged.merge(incoming) if merged else incoming
        if category is not None:
            from gallery_dl import extractor
            classes = extractor.extractors()
            categories = {cls.category for cls in classes} | {cls.basecategory for cls in classes} | {"coomer", "ytdl-generic"}
            # oauth is also an extractor, but this root key is a shared client
            # configuration object used by the other authenticated extractors.
            categories.discard("oauth")
            if category not in categories:
                raise InvalidData("Unknown source category for a scoped worker profile")
            section = merged.value.get("extractor")
            if section is None or not isinstance(section.value, dict):
                raise InvalidData("Scoped conversion requires an extractor configuration object")
            # These pinned adapters stay within their root source. Reddit
            # can safely narrow its dependencies with the existing finite host
            # whitelist. Unknown dependency graphs retain all configured sites.
            included = {category} if category in ("twitter", "instagram", "coomer", "kemono", "bluesky", "tiktok") else None
            if category == "reddit":
                settings = section.plain()
                whitelist = settings.get("reddit", {}).get("whitelist", settings.get("whitelist"))
                if isinstance(whitelist, str):
                    whitelist = whitelist.split(",")
                if isinstance(whitelist, list) and all(isinstance(name, str) for name in whitelist):
                    hosts = {name.split(":", 1)[0] for name in whitelist}
                    if hosts <= {"reddit", "imgur", "redgifs", "directlink"}:
                        included = hosts | {"reddit"}
            kept = {}
            for key, value in section.value.items():
                source = key.split(">", 1)[0]
                if included is not None and (source in categories or ">" in key) and source not in included | {"*"}:
                    self.excluded.append("/extractor/" + key)
                else:
                    kept[key] = value
            merged.value["extractor"] = Setting(kept, section.sources)
            registry = merged.value.get("postprocessor")
            if registry is not None and isinstance(registry.value, dict):
                needed = set()

                def references(value):
                    if isinstance(value, dict):
                        for key, child in value.items():
                            if key == "postprocessors" and isinstance(child, list):
                                for entry in child:
                                    if isinstance(entry, str):
                                        needed.add(entry)
                                    elif isinstance(entry, dict) and isinstance(entry.get("type"), str):
                                        needed.add(entry["type"])
                            references(child)
                    elif isinstance(value, list):
                        for child in value:
                            references(child)

                references({key: child.plain() for key, child in merged.value.items() if key != "postprocessor"})
                kept = {}
                for key, value in registry.value.items():
                    if key in needed:
                        kept[key] = value
                    else:
                        self.excluded.append("/postprocessor/" + key)
                merged.value["postprocessor"] = Setting(kept, registry.sources)
        self.settings = merged
        self.named = merged.plain().get("postprocessor", {})
        if not isinstance(self.named, dict):
            raise InvalidData("Named postprocessors must be a JSON object")

    def bind(self, kind, position, **definition):
        name = kind + "_" + digest(pointer(position).encode())[:20]
        binding = {"kind": kind, **definition}
        if name in self.bindings and self.bindings[name] != binding:
            raise InvalidData("Conflicting generated worker binding")
        if len(self.bindings) >= 64 and name not in self.bindings:
            raise InvalidData("Converted configuration exceeds the worker binding limit")
        self.bindings[name] = binding
        return "${stash:" + name + "}"

    def private(self, setting, position):
        if len(setting.sources) == 1:
            return self.bind("private", position, **setting.sources[0])
        return self.bind("private", position, sources=setting.sources)

    def local_path(self, value):
        if not isinstance(value, str) or not value or "${stash:" in value:
            raise InvalidData("Expected an ordinary local gallery-dl path")
        expanded = os.path.expandvars(os.path.expanduser(value))
        if "$" in expanded or any(ord(c) < 32 for c in expanded):
            raise InvalidData("A local gallery-dl path has an unresolved environment reference")
        return _path(expanded, self.working)

    def path_setting(self, value, position):
        if value is None or type(value) is bool or value == "":
            return value
        if isinstance(value, str):
            if "{" in value or "://" in value:
                raise InvalidData("Formatted or remote string paths require explicit configuration review")
            return self.bind("path", position, path=str(self.local_path(value)))
        if not isinstance(value, list) or not value or any(not isinstance(part, str) or not part for part in value):
            raise InvalidData("A gallery-dl path must be a string or nonempty segment list")
        first = value[0]
        if first.startswith(":") and (":basedirectory".startswith(first) or ":directory".startswith(first)):
            return copy.deepcopy(value)
        if first.startswith(":~"):
            first = os.path.expanduser(first[1:])
        elif first.startswith(":$"):
            first = os.environ.get(first[2:])
            if not first:
                raise InvalidData("A gallery-dl path environment reference is unavailable")
        elif first.startswith(":"):
            raise InvalidData("Unknown gallery-dl path root requires explicit review")
        count = 1
        if "{" in first:
            first, count = str(self.working), 0
        while count < len(value) and "{" not in value[count]:
            segment = value[count]
            if not re.fullmatch(r"[A-Za-z0-9_ .-]+", segment) or segment in {".", ".."}:
                raise InvalidData("A formatted path's static segments require explicit review")
            first = os.path.join(first, segment)
            count += 1
        return [self.bind("path", position, path=str(self.local_path(first))), *value[count:]]

    def asset(self, value, position):
        path = self.local_path(value)
        with open(path, "rb") as source:
            raw = source.read(ASSET_LIMIT + 1)
        if not 0 < len(raw) <= ASSET_LIMIT:
            raise InvalidData("A conversion helper must be a bounded local file")
        sha = digest(raw)
        _asset(path, sha)
        return self.bind("asset", position, path=str(path), sha256=sha)

    def processor(self, value):
        if isinstance(value, str):
            return self.named.get(value) or {"name": value}
        if isinstance(value, dict):
            if "type" in value:
                return {**(self.named.get(value["type"]) or {}), **value}
            return value
        raise InvalidData("Invalid postprocessor entry")

    def walk(self, setting, position=()):
        value = setting.value
        if isinstance(value, dict):
            result = {}
            for key, child in value.items():
                at = (*position, key)
                plain = child.plain()
                if _access_option(key) and plain and not isinstance(plain, bool):
                    result[key] = self.private(child, at)
                elif key == "base-directory":
                    if not isinstance(plain, str) or not plain:
                        raise InvalidData("Conversion requires an explicit local base directory")
                    try:
                        relative = self.local_path(plain).relative_to(self.root.path).as_posix()
                    except ValueError:
                        raise InvalidData("A configured base directory is outside the reviewed media root") from None
                    result[key] = "${stash:media_root}" + ("/" + relative if relative != "." else "")
                elif key in {"archive", "part-directory", "logfile", "unsupportedfile"}:
                    result[key] = self.path_setting(plain, at)
                elif key == "function" and plain:
                    module, separator, function = plain.rpartition(":") if isinstance(plain, str) else ("", "", "")
                    if not separator or not function.isidentifier() or "gallery_catalog_hook" in module:
                        raise InvalidData("A Python processor requires explicit conversion")
                    result[key] = self.asset(module, at) + ":" + function
                elif key == "command" and plain:
                    if not isinstance(plain, list) or any(not isinstance(arg, str) for arg in plain):
                        raise InvalidData("Convert shell-string processors to reviewed argument arrays first")
                    args = []
                    for index, arg in enumerate(plain):
                        local = "{" not in arg and (arg.startswith(("/", "~/", "./")))
                        if local and (Path(arg).suffix in {".py", ".sh"} or self.local_path(arg) in self.explicit_assets):
                            arg = self.asset(arg, (*at, index))
                        args.append(arg)
                    result[key] = args
                elif key == "cmdline-args" and plain:
                    result[key] = [self.private(item, (*at, index)) if private else self.walk(item, (*at, index))
                                   for index, (item, private) in enumerate(zip(child.value, private_arguments(plain)))]
                elif key == "postprocessors":
                    if not isinstance(child.value, list):
                        raise InvalidData("Postprocessor lists require explicit conversion")
                    result[key] = []
                    for index, item in enumerate(child.value):
                        if legacy_processor(self.processor(item.plain())):
                            self.removed.append(pointer((*at, index)))
                        else:
                            result[key].append(self.walk(item, (*at, index)))
                elif position == ("postprocessor",) and legacy_processor(plain):
                    self.removed.append(pointer(at))
                else:
                    result[key] = self.walk(child, at)
            return result
        if isinstance(value, list):
            return [self.walk(child, (*position, index)) for index, child in enumerate(value)]
        return value

    def convert(self):
        self.bindings, self.removed = {}, []
        gallery = self.walk(self.settings)
        if "base-directory" not in gallery.get("extractor", {}):
            raise InvalidData("Conversion requires an explicit extractor base directory")
        result = {"schema": SCHEMA, "root": self.root_definition, "locks": self.lock_definition,
                  "gallery": gallery, "bindings": self.bindings}
        if self.category is not None:
            result["source_category"] = self.category
        profile_bytes(result)
        return result

    def report(self):
        return {"removed_catalog_processors": self.removed, "excluded_outside_category": self.excluded,
                "bindings": {kind: sum(value["kind"] == kind for value in self.bindings.values())
                             for kind in ("path", "asset", "private")}}


def publish_profile(destination, value):
    """Publish a new private file atomically; never overwrite live configuration."""
    destination = Path(destination).absolute()
    body = profile_bytes(value)
    fd, temporary = tempfile.mkstemp(prefix=".native-worker-", dir=destination.parent)
    try:
        with os.fdopen(fd, "wb") as output:
            output.write(body)
            output.flush()
            os.fsync(output.fileno())
        os.link(temporary, destination)
        directory = os.open(destination.parent, os.O_RDONLY | os.O_DIRECTORY)
        try:
            os.fsync(directory)
        finally:
            os.close(directory)
    finally:
        os.unlink(temporary)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", action="append", required=True, help="Ordered existing JSON layer; may be repeated")
    parser.add_argument("--root", required=True, help="Reviewed native media-root UUID")
    parser.add_argument("--root-path", required=True)
    parser.add_argument("--root-identity", nargs=2, type=int, required=True, metavar=("DEVICE", "INODE"))
    parser.add_argument("--locks", required=True)
    parser.add_argument("--lock-identity", nargs=2, type=int, required=True, metavar=("DEVICE", "INODE"))
    parser.add_argument("--working-directory", default=os.getcwd(), help="Original gallery-dl working directory")
    parser.add_argument("--asset", action="append", default=[], help="Additional local exec helper to fingerprint")
    parser.add_argument("--category", help="Scope settings and helper definitions to one root extractor category")
    parser.add_argument("--source-mode", choices=("published", "traversal"), default="published",
                        help="Publication-date window or configured scan without publication coverage")
    parser.add_argument("--full-history", action="store_true", help="Preserve the wrappers' global skip=true override in this profile")
    parser.add_argument("--output", required=True, help="New, inactive profile file; must not exist")
    args = parser.parse_args(argv)
    try:
        converter = Converter(args.config, {"uuid": args.root, "path": args.root_path, "identity": args.root_identity},
                              {"path": args.locks, "identity": args.lock_identity}, working_directory=args.working_directory,
                              assets=args.asset, category=args.category)
        value = converter.convert()
        value['source_mode'] = args.source_mode
        if args.full_history:
            value["gallery"]["skip"] = True
        checked = Configuration.from_document(value, Path(args.output).absolute().parent)
        publish_profile(args.output, value)
        print(encode({**converter.report(), "state": "converted", "policy_sha256": checked.policy_sha256,
                      "root_uuid": checked.root_uuid}).decode())
        return 0
    except InvalidData as error:
        print(str(error), file=sys.stderr)
    except FileExistsError:
        print("Output already exists; existing configuration was not replaced", file=sys.stderr)
    except OSError:
        print("Configuration files, helpers or output storage are unavailable", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
