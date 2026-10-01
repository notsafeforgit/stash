import copy
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

try:
    from gallery_dl import config, exception, path
    from stash_ingest import filename as names
    HAS_GALLERY = True
except ImportError:
    HAS_GALLERY = False


@unittest.skipUnless(HAS_GALLERY, 'Requires installed gallery-dl Python')
class FilenameBudgetTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        config.clear();self.addCleanup(config.clear)

    def make(self,template,fields=None,processors=()):
        data={'id':'post123','num':12,'count':2,'title':'Short title','extension':'jpg','category':'fixture'}
        data.update(fields or {})
        options={'filename':template,'base-directory':str(self.root),'path-restrict':'auto',
                 'path-remove':'\\u0000-\\u001f\\u007f','postprocessors':list(processors)}
        extractor=SimpleNamespace(config=lambda key,default=None:options.get(key,default),
            config_accumulate=lambda key:options.get(key,()),_parentdir=None,directory_fmt=[],filename_fmt='{id}.{extension}')
        pf=path.PathFormat(extractor);pf.set_directory(data);pf.set_filename(data)
        job=SimpleNamespace(extractor=extractor,pathfmt=pf,archive=None,get_downloader=lambda _:None)
        original=pf.build_filename(data)
        budget=names.install(job);budget.begin(data)
        return job,data,original,budget

    def assertFits(self,job):
        pf=job.pathfmt;pf.build_path()
        self.assertLessEqual(len(pf.filename.encode()),255)
        Path(pf.realpath).write_bytes(b'fixture')
        pf.part_enable();self.assertLessEqual(len(Path(pf.temppath).name.encode()),255)
        Path(pf.temppath).write_bytes(b'partial')
        return pf.filename

    def test_existing_short_templates_keep_identical_names(self):
        for template in ['{id}_{num}_{title}.{extension}','{title}_{id}_{num}.{extension}',
                         '{title!W:Xb230/.../}_#{num}.{extension}',{ 'count > 1':'{id}_{num}.{extension}','':'{id}.{extension}'}]:
            job,data,original,_=self.make(template)
            self.assertEqual(job.pathfmt.build_filename(data),original)

    def test_multibyte_title_reserves_suffix_identifiers_and_extension(self):
        original='漢字🙂é'*200
        job,data,old,_=self.make('{title}_{id}_{num}.{extension}',{'title':original,'id':'12345678901234567890','num':123456789,'extension':'webm'})
        filename=self.assertFits(job)
        self.assertGreater(len(old.encode()),255)
        self.assertTrue(filename.endswith('_12345678901234567890_123456789.webm'))
        self.assertEqual(data['title'],original)
        self.assertNotIn('\ufffd',filename)

    def test_reddit_redgifs_prefix_is_reserved_before_title(self):
        metadata={'_reddit':{'id':'1234567890123','title':'Long title '*100},'id':'VeryLongRedgifsIdentifierWithAdditionalWords','extension':'mp4'}
        job,data,old,_=self.make('{_reddit[id]}_{id}_{num}_{_reddit[title]!W:Xb198/.../}.{extension}',metadata)
        before=copy.deepcopy(data)
        filename=self.assertFits(job)
        self.assertGreater(len(old.encode())+5,255)
        self.assertTrue(filename.startswith('1234567890123_VeryLongRedgifsIdentifierWithAdditionalWords_12_'))
        self.assertTrue(filename.endswith('.mp4'))
        self.assertEqual(data,before)

    def test_gif_temp_name_fits_with_random_suffix(self):
        processor={'name':'exec','command':['python3','/fixture/gif_to_av1_qsv.py','{_path}']}
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'界'*200,'extension':'gif'},[processor])
        filename=self.assertFits(job)
        converted=Path(filename).with_suffix('.mkv').name
        temporary='.'+converted+'.12345678.part'
        self.assertLessEqual(len(temporary.encode()),255)
        (self.root/temporary).write_bytes(b'converted fixture')
        self.assertTrue(converted.endswith('_post123_12.mkv'))

    def test_exiftool_temporary_suffix_is_reserved(self):
        pp={'name':'exec','command':['/usr/bin/exiftool','-overwrite_original','{_path}']}
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*500},[pp])
        filename=self.assertFits(job)
        self.assertLessEqual(len((filename+'_exiftool_tmp').encode()),255)

    def test_title_only_template_gets_stable_identity_suffix_when_shortened(self):
        template='{title!W:Xb237/.../}_#{num}.{extension}'
        seen=[]
        for ident in ('first','second'):
            job,data,_,budget=self.make(template,{'title':'x'*500,'id':ident,'extension':'mp4'})
            budget.info={'ext':'mp4','protocol':'m3u8_native'}
            filename=job.pathfmt.build_filename(data);seen.append(filename)
            self.assertRegex(filename,r'~[0-9a-f]{16}_#12\.mp4$')
            self.assertEqual(job.pathfmt.build_filename(data),filename)
            self.assertLessEqual(len(filename.encode())+5,255)
        self.assertNotEqual(*seen)

    def test_utf8_budget_uses_filesystem_limit(self):
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'🙂'*100})
        with patch.object(names,'name_max',return_value=100):
            filename=job.pathfmt.build_filename(data)
        self.assertLessEqual(len(filename.encode())+5,100)
        self.assertTrue(filename.endswith('_post123_12.jpg'))

    def test_identifiers_are_never_silently_cut(self):
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'Title','id':'i'*300})
        with self.assertRaises(exception.FilenameFormatError):job.pathfmt.build_filename(data)

    def test_auxiliary_and_extensionless_paths_share_the_same_stem(self):
        processor={'name':'metadata','mode':'custom','extension-format':'txt'}
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'界'*500,'extension':'webm'},[processor])
        pf=job.pathfmt;full=pf.build_filename(data)
        blank=pf.build_filename(dict(data,extension=''))
        sidecar=pf.build_filename(dict(data,extension='txt'))
        self.assertEqual(blank+'webm',full)
        self.assertEqual(Path(sidecar).stem,Path(full).stem)

    def test_resolved_extension_rebudgets_title_but_keeps_item_number(self):
        job,data,_,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*500,'extension':'jpg'})
        pf=job.pathfmt;short=pf.build_filename(data)
        pf.set_extension('longerextension');long=pf.build_filename(data)
        self.assertLessEqual(len(long.encode())+5,255)
        self.assertTrue(long.endswith('_post123_12.longerextension'))
        self.assertLess(len(Path(long).stem),len(Path(short).stem))

    def test_existing_safe_partial_is_resumed_under_same_name(self):
        job,data,old,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*220})
        (self.root/(old+'.part')).write_bytes(b'keep partial bytes')
        job.pathfmt.build_path();job.pathfmt.part_enable()
        self.assertEqual(job.pathfmt.filename,old)
        self.assertEqual(Path(job.pathfmt.temppath).read_bytes(),b'keep partial bytes')

    def test_unsafe_legacy_partial_is_reported_and_preserved(self):
        pp={'name':'exec','command':['python3','/fixture/gif_to_av1_qsv.py','{_path}']}
        job,data,old,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*227,'extension':'gif'},[pp])
        old_part=self.root/(old+'.part');old_part.write_bytes(b'legacy partial')
        with self.assertRaises(exception.FilenameFormatError):job.pathfmt.build_filename(data)
        self.assertEqual(old_part.read_bytes(),b'legacy partial')

    def test_completed_archived_legacy_media_keeps_its_name(self):
        pp={'name':'exec','command':['python3','/fixture/gif_to_av1_qsv.py','{_path}']}
        job,data,old,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*227,'extension':'gif'},[pp])
        (self.root/old).with_suffix('.mkv').write_bytes(b'completed conversion')
        job.archive=SimpleNamespace(check=lambda _:True)
        self.assertEqual(job.pathfmt.build_filename(data),old)

    def test_legacy_unarchived_media_is_not_silently_downloaded_again(self):
        pp={'name':'exec','command':['python3','/fixture/gif_to_av1_qsv.py','{_path}']}
        job,data,old,_=self.make('{title}_{id}_{num}.{extension}',{'title':'x'*227,'extension':'gif'},[pp])
        converted=(self.root/old).with_suffix('.mkv')
        converted.write_bytes(b'completed but not acknowledged')
        with self.assertRaises(exception.FilenameFormatError):job.pathfmt.build_filename(data)
        self.assertEqual(converted.read_bytes(),b'completed but not acknowledged')

    def test_ytdl_reserves_format_ids_and_nested_fragment_part_suffix(self):
        job,data,_,budget=self.make('{title} [{id}]_#{num}.{extension}',{'title':'界'*500,'extension':'mkv'})
        info={'ext':'mkv','requested_formats':[{'ext':'mp4','format_id':'AVC_very_long_stream_identifier_123','protocol':'http_dash_segments'},
            {'ext':'m4a','format_id':'AAC_audio_stream_4','protocol':'m3u8_native'}]}
        budget.info=info
        pf=job.pathfmt;full=pf.build_filename(data);base=pf.build_filename(dict(data,extension=''))
        self.assertEqual(base+'mkv',full)
        for item in info['requested_formats']:
            stem=base[:-1]
            fragment=f'{stem}.f{item["format_id"]}.{item["ext"]}.part-Frag'+('9'*20)+'.part'
            self.assertLessEqual(len(fragment.encode()),255)
            (self.root/fragment).write_bytes(b'fragment fixture')
        self.assertTrue(full.endswith(' [post123]_#12.mkv'))

    def test_ytdl_ext_field_accounts_for_appended_extension(self):
        job,data,_,budget=self.make('{title[:230]} [{id}].{ext[:10]}',{'title':'界'*500,'extension':'mp4','ext':'mp4'})
        budget.info={'ext':'mp4','protocol':'http'}
        filename=job.pathfmt.build_filename(data)
        # Installed gallery-dl appends %(ext)s after the already-rendered {ext}.
        self.assertLessEqual(len((filename+'mp4.part').encode()),255)
        self.assertTrue(filename.endswith(' [post123].mp4'))

    def test_real_ytdl_downloader_uses_guard_after_format_resolution(self):
        from gallery_dl.downloader.ytdl import YoutubeDLDownloader
        from yt_dlp import YoutubeDL
        job,data,_,budget=self.make('{title}_{id}_{num}.{extension}',{'title':'界'*500,'extension':None})
        downloader=YoutubeDLDownloader.__new__(YoutubeDLDownloader)
        downloader.outtmpl=None;downloader.rate_dyn=None;downloader.part=True;downloader.partdir=None
        downloader.out=SimpleNamespace(start=lambda _:None)
        # Install a new job so its downloader factory is intercepted normally.
        job.get_downloader=lambda _:downloader
        del job._stash_filename_budget
        job.pathfmt.build_filename=budget.original
        budget=names.install(job)
        info={'id':'post123','title':data['title'],'ext':'mp4','protocol':'m3u8_native','url':'https://fixture.invalid/video.mp4'}
        writes=[]
        with YoutubeDL({'quiet':True,'no_warnings':True}) as yt:
            def process(value):
                filename=yt.prepare_filename(value)
                fragment=filename+'.part-Frag'+('9'*20)+'.part'
                self.assertLessEqual(len(Path(fragment).name.encode()),255)
                Path(fragment).write_bytes(b'fixture fragment')
                Path(filename).write_bytes(b'fixture media')
                value['filepath']=filename;writes.append(filename)
            yt.process_info=process
            wrapped=job.get_downloader('ytdl')
            with patch('requests.sessions.Session.request',side_effect=AssertionError('No network')):
                self.assertTrue(wrapped._download_video(yt,job.pathfmt,info))
        self.assertEqual(len(writes),1)
        self.assertEqual(Path(writes[0]).name,job.pathfmt.filename)
        self.assertTrue(job.pathfmt.filename.endswith('_post123_12.mp4'))

    def test_real_ytdl_playlist_budgets_each_lazy_entry(self):
        from gallery_dl.downloader.ytdl import YoutubeDLDownloader
        from yt_dlp import YoutubeDL
        job,data,_,budget=self.make('{title}_{id}_{num}.{extension}',{'title':'界'*500,'extension':'mp4'})
        downloader=YoutubeDLDownloader.__new__(YoutubeDLDownloader)
        downloader.outtmpl=None;downloader.rate_dyn=None
        job.get_downloader=lambda _:downloader
        del job._stash_filename_budget
        job.pathfmt.build_filename=budget.original
        budget=names.install(job)
        processed=[]
        def entries():
            for index in (1,12345678901234567890):
                if index!=1:self.assertEqual(processed,[1], 'Playlist was consumed ahead of download')
                yield {'id':str(index),'title':data['title'],'playlist_index':index,
                       'ext':'mp4','protocol':'m3u8_native'}
        with YoutubeDL({'quiet':True,'no_warnings':True}) as yt:
            def process(value):
                filename=yt.prepare_filename(value)
                fragment=filename+'.part-Frag'+('9'*20)+'.part'
                self.assertLessEqual(len(Path(fragment).name.encode()),255)
                rendered_index=yt.evaluate_outtmpl('%(playlist_index)s',value)
                self.assertTrue(Path(filename).name.endswith(f'_post123_12.{rendered_index}.mp4'),Path(filename).name)
                Path(fragment).write_bytes(b'fixture fragment')
                processed.append(value['playlist_index'])
            yt.process_info=process
            wrapped=job.get_downloader('ytdl')
            with patch('requests.sessions.Session.request',side_effect=AssertionError('No network')):
                self.assertTrue(wrapped._download_playlist(yt,job.pathfmt,{'entries':entries()}))
        self.assertEqual(processed,[1,12345678901234567890])
        self.assertIsNone(budget.playlist_index)

    def test_actual_ytdlp_merge_paths_fit_even_with_ext_template(self):
        from gallery_dl.downloader.ytdl import YoutubeDLDownloader
        from yt_dlp import YoutubeDL
        from yt_dlp.postprocessor.ffmpeg import FFmpegMergerPP
        for template in ('{title} [{id}].{extension}', '{title[:230]} [{id}].{ext[:10]}'):
            with self.subTest(template=template):
                job,data,_,budget=self.make(template,{'title':'界'*500,'extension':None,'ext':'mp4'})
                downloader=YoutubeDLDownloader.__new__(YoutubeDLDownloader)
                downloader.outtmpl=None;downloader.rate_dyn=None;downloader.part=True;downloader.partdir=None
                downloader.out=SimpleNamespace(start=lambda _:None)
                job.get_downloader=lambda _:downloader
                del job._stash_filename_budget
                job.pathfmt.build_filename=budget.original
                names.install(job)
                info={'id':'post123','title':data['title'],'ext':'mp4', 'extractor':'fixture',
                      'requested_formats':[
                          {'ext':'mp4','format_id':'video_with_a_long_format_id_12345','protocol':'http_dash_segments','url':'https://fixture.invalid/v'},
                          {'ext':'m4a','format_id':'audio_678','protocol':'m3u8_native','url':'https://fixture.invalid/a'}]}
                writes=[]
                with YoutubeDL({'quiet':True,'no_warnings':True,'fixup':'never'}) as yt:
                    def download(filename,value,*args,**kwargs):
                        fragment=filename+'.part-Frag'+('9'*20)+'.part'
                        self.assertLessEqual(len(Path(fragment).name.encode()),255)
                        Path(fragment).write_bytes(b'fixture fragment')
                        Path(filename).write_bytes(b'fixture stream')
                        writes.append(filename)
                        return True, True
                    def postprocess(filename,value,files_to_move=None):
                        temporary=str(Path(filename).with_suffix('.temp'+Path(filename).suffix))
                        self.assertLessEqual(len(Path(temporary).name.encode()),255)
                        Path(temporary).write_bytes(b'fixture merge')
                        value['filepath']=filename
                        return value
                    yt.dl=download;yt.post_process=postprocess
                    with patch.object(FFmpegMergerPP,'available',True), \
                         patch('subprocess.Popen',side_effect=AssertionError('No external commands')), \
                         patch('requests.sessions.Session.request',side_effect=AssertionError('No network')):
                        self.assertTrue(job.get_downloader('ytdl')._download_video(yt,job.pathfmt,info))
                self.assertEqual(len(writes),2)


if __name__=='__main__':unittest.main()
