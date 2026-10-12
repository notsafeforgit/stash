// Audio ordinal within one input, used by both ffprobe and the concat graph.
// iPhone spatial recordings include an APAC track plus an AAC stereo rendition.
// Their order (and default disposition) can change when Photos exports an edit.
System.Func<System.Text.Json.JsonElement, bool, int> selectPhoneAudio = (streams, phoneUpload) =>
{
    int count = streams.GetArrayLength();
    if (count == 0) return -1;
    if (count == 1)
    {
        var stream = streams[0];
        if (!stream.TryGetProperty("codec_name", out var codec) || string.IsNullOrEmpty(codec.GetString()) ||
            (stream.TryGetProperty("codec_tag_string", out var tag) && tag.GetString() == "apac"))
            throw new System.Exception("The clip has no supported audio rendition.");
        return 0;
    }
    if (!phoneUpload)
        throw new System.Exception("Multiple audio tracks need an explicit selection.");
    int selected = -1, spatial = 0, index = 0;
    foreach (var stream in streams.EnumerateArray())
    {
        if (stream.TryGetProperty("codec_tag_string", out var tag) && tag.GetString() == "apac")
            spatial++;
        else if (stream.TryGetProperty("codec_name", out var codec) && codec.GetString() == "aac" &&
                 stream.TryGetProperty("channels", out var channels) && channels.GetInt32() == 2 && selected < 0)
            selected = index;
        else
            throw new System.Exception("Multiple audio tracks do not identify one iPhone stereo rendition.");
        index++;
    }
    if (selected < 0 || spatial == 0)
        throw new System.Exception("Spatial recording is missing its AAC stereo rendition.");
    return selected;
};
