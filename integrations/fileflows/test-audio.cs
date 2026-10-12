// Execute after select-audio.cs in a FileFlows C# test node.
int audioChecks = 0;
System.Action<string, string, bool, int> checkAudio = (name, json, phone, expected) =>
{
    using var document = System.Text.Json.JsonDocument.Parse(json);
    int actual = int.MinValue;
    try { actual = selectPhoneAudio(document.RootElement, phone); }
    catch (System.Exception) { if (expected != int.MinValue) throw; }
    if (actual != expected) throw new System.Exception("Audio selection failed: " + name);
    audioChecks++;
};
const string stereo = "{\"codec_name\":\"aac\",\"codec_tag_string\":\"mp4a\",\"channels\":2}";
const string spatial = "{\"codec_tag_string\":\"apac\",\"channels\":4}";
const string mono = "{\"codec_name\":\"aac\",\"channels\":1}";
checkAudio("silent", "[]", true, -1);
checkAudio("ordinary stereo", "[" + stereo + "]", true, 0);
checkAudio("ordinary mono", "[" + mono + "]", true, 0);
checkAudio("spatial first", "[" + spatial + "," + stereo + "]", true, 1);
checkAudio("stereo first", "[" + stereo + "," + spatial + "]", true, 0);
checkAudio("spatial without fallback", "[" + spatial + "]", true, int.MinValue);
checkAudio("ambiguous stereo tracks", "[" + stereo + "," + stereo + "]", true, int.MinValue);
checkAudio("unrelated alternate audio", "[" + stereo + "," + mono + "]", true, int.MinValue);
checkAudio("unknown single codec", "[{\"channels\":2}]", true, int.MinValue);
checkAudio("other library stereo", "[" + stereo + "]", false, 0);
checkAudio("other library multiple tracks", "[" + spatial + "," + stereo + "]", false, int.MinValue);
Logger.ILog("Phone audio selection checks passed: " + audioChecks);
