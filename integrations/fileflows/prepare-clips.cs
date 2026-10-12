// Ordered phone uploads. This node precedes the existing adaptive concat pipeline.
// The caller supplies sourceFolder; no Stash credentials are present in FileFlows.
try
{
    string folder = Variables.ContainsKey("folder.Orig.FullName")
        ? Variables["folder.Orig.FullName"].ToString() : Variables["file.FullName"].ToString();
    var dir = new System.IO.DirectoryInfo(folder);
    string manifestPath = System.IO.Path.Combine(dir.FullName, "stash-clips.json");
    if (!System.IO.File.Exists(manifestPath))
    {
        Logger.ILog("Waiting for the upload manifest; retrying in five minutes.");
        return 2;
    }
    if (dir.Parent == null || (dir.Attributes & System.IO.FileAttributes.ReparsePoint) != 0 ||
        System.IO.Directory.GetDirectories(dir.FullName).Length != 0)
        throw new System.Exception("Expected a regular, flat upload folder.");
    if (new System.IO.FileInfo(manifestPath).Length > 262144)
        throw new System.Exception("Upload manifest is too large.");
    var manifestInfo = new System.IO.FileInfo(manifestPath);
    long manifestLength = manifestInfo.Length, manifestTicks = manifestInfo.LastWriteTimeUtc.Ticks;
    using var json = System.Text.Json.JsonDocument.Parse(System.IO.File.ReadAllText(manifestPath));
    var root = json.RootElement;
    if (root.GetProperty("format").GetString() != "stash-clip-batch-v1")
        throw new System.Exception("Unsupported upload manifest.");
    string order = root.TryGetProperty("order", out var orderValue) ? orderValue.GetString() : "recorded_at";
    if (order != "recorded_at" && order != "input")
        throw new System.Exception("Unsupported clip order; expected recorded_at or input.");
    string batch = root.GetProperty("batch_id").GetString();
    if (!System.Text.RegularExpressions.Regex.IsMatch(batch ?? "", @"^[A-Za-z0-9-]{16,80}$") ||
        dir.Name != "stash-upload-" + batch + ".part")
        throw new System.Exception("Upload folder does not match its batch ID.");
    string title = root.GetProperty("title").GetString();
    string details = root.GetProperty("details").GetString();
    if (string.IsNullOrWhiteSpace(title) || System.Text.Encoding.UTF8.GetByteCount(title) > 4096 ||
        details == null || System.Text.Encoding.UTF8.GetByteCount(details) > 16384 || title.Contains('\0') || details.Contains('\0'))
        throw new System.Exception("Invalid upload title or description.");
    var clips = new System.Collections.Generic.List<System.Tuple<string, System.DateTimeOffset, int>>();
    var names = new System.Collections.Generic.HashSet<string>(System.StringComparer.OrdinalIgnoreCase);
    var snapshots = new System.Collections.Generic.Dictionary<string, System.Tuple<long,long>>(System.StringComparer.Ordinal);
    var entries = root.GetProperty("clips");
    // Shortcuts can coerce a one-item Repeat Results list to its dictionary.
    var clipEntries = new System.Collections.Generic.List<System.Text.Json.JsonElement>();
    if (entries.ValueKind == System.Text.Json.JsonValueKind.Array)
        foreach (var entry in entries.EnumerateArray()) clipEntries.Add(entry);
    else if (entries.ValueKind == System.Text.Json.JsonValueKind.Object) clipEntries.Add(entries);
    if (clipEntries.Count < 1 || clipEntries.Count > 200)
        throw new System.Exception("Expected 1–200 clips.");
    int index = 0;
    foreach (var item in clipEntries)
    {
        string name = item.GetProperty("name").GetString();
        string digest = item.GetProperty("sha256").GetString();
        string recorded = item.GetProperty("recorded_at").GetString();
        if (string.IsNullOrEmpty(name) || name != System.IO.Path.GetFileName(name) || name.Contains('\\') ||
            !System.Text.RegularExpressions.Regex.IsMatch(name, @"^clip-[0-9]+\.(mov|mp4|m4v)$", System.Text.RegularExpressions.RegexOptions.IgnoreCase) ||
            !names.Add(name) || !System.Text.RegularExpressions.Regex.IsMatch(digest ?? "", @"^[a-fA-F0-9]{64}$"))
            throw new System.Exception("Invalid or repeated clip name/hash.");
        System.DateTimeOffset date;
        if (!System.Text.RegularExpressions.Regex.IsMatch(recorded ?? "", @"(Z|[+-][0-9]{2}:[0-9]{2})$") ||
            !System.DateTimeOffset.TryParse(recorded, System.Globalization.CultureInfo.InvariantCulture,
                System.Globalization.DateTimeStyles.None, out date))
            throw new System.Exception("Every clip needs its recording date with timezone.");
        string path = System.IO.Path.Combine(dir.FullName, name);
        var info = new System.IO.FileInfo(path);
        if (!info.Exists || info.Length < 1)
        {
            Logger.ILog("A clip has not arrived; retrying in five minutes.");
            return 2;
        }
        if ((info.Attributes & System.IO.FileAttributes.ReparsePoint) != 0)
            throw new System.Exception("A clip is not a regular file.");
        long size = info.Length, ticks = info.LastWriteTimeUtc.Ticks;
        string actual;
        using (var file = System.IO.File.OpenRead(path))
        using (var sha = System.Security.Cryptography.SHA256.Create())
            actual = System.Convert.ToHexString(sha.ComputeHash(file));
        info.Refresh();
        if (actual != digest.ToUpperInvariant() || info.Length != size || info.LastWriteTimeUtc.Ticks != ticks)
        {
            Logger.ILog("A clip is incomplete or changed; retaining originals and retrying in five minutes.");
            return 2;
        }
        snapshots.Add(name, System.Tuple.Create(size, ticks));
        clips.Add(System.Tuple.Create(name, date, index++));
    }
    if (System.IO.Directory.GetFiles(dir.FullName).Length != clips.Count + 1)
        throw new System.Exception("Upload contains unexpected files; refusing cleanup.");
    manifestInfo.Refresh();
    if (manifestInfo.Length != manifestLength || manifestInfo.LastWriteTimeUtc.Ticks != manifestTicks ||
        (manifestInfo.Attributes & System.IO.FileAttributes.ReparsePoint) != 0)
        throw new System.Exception("Manifest changed while checking the clips.");
    snapshots.Add("stash-clips.json", System.Tuple.Create(manifestLength, manifestTicks));
    var earliest = clips[0].Item2;
    foreach (var clip in clips) if (clip.Item2 < earliest) earliest = clip.Item2;
    if (order == "recorded_at")
        clips.Sort((a,b) => { int c = a.Item2.CompareTo(b.Item2); return c != 0 ? c : a.Item3.CompareTo(b.Item3); });
    string dateText = earliest.ToString("yyyy-MM-dd", System.Globalization.CultureInfo.InvariantCulture);
    if (root.TryGetProperty("date", out var customDate) && !string.IsNullOrEmpty(customDate.GetString()))
    {
        dateText = customDate.GetString();
        if (!System.DateTime.TryParseExact(dateText, "yyyy-MM-dd", System.Globalization.CultureInfo.InvariantCulture,
            System.Globalization.DateTimeStyles.None, out var ignored)) throw new System.Exception("Invalid scene date.");
    }
    string safeTitle = System.Text.RegularExpressions.Regex.Replace(title, @"[\x00-\x1f/\\:*?""<>|]", "_").Trim().TrimEnd('.');
    while (System.Text.Encoding.UTF8.GetByteCount(safeTitle) > 120) safeTitle = safeTitle.Substring(0, safeTitle.Length-1);
    if (safeTitle.Length > 0 && char.IsHighSurrogate(safeTitle[safeTitle.Length-1])) safeTitle = safeTitle.Substring(0, safeTitle.Length-1);
    if (safeTitle.Length == 0) safeTitle = "Phone video";
    string basename = safeTitle + " [" + batch + "]";
    string target = System.IO.Path.Combine(dir.Parent.FullName, basename + ".mkv");
    string handoff = System.IO.Path.Combine(dir.Parent.FullName, "stash-import-" + batch + ".json");
    if (System.IO.File.Exists(target) || System.IO.File.Exists(handoff))
        throw new System.Exception("This upload already has a published output; inspect its pending import instead of overwriting it.");
    Variables["sourceFolder"] = dir.FullName;
    Variables["targetParent"] = dir.Parent.FullName;
    Variables["targetBaseName"] = basename;
    Variables["targetVideoPath"] = target;
    Variables["sourceMediaKind"] = "ordered-clips";
    Variables["publishNfo"] = "false";
    Variables["sourceClipCount"] = clips.Count.ToString();
    Variables["sourceFileCount"] = snapshots.Count.ToString();
    Variables["stashImportPath"] = handoff;
    Variables["stashImportManifest"] = System.Text.Json.JsonSerializer.Serialize(new {
        format = "stash-clip-import-v1", batch_id = batch, file = System.IO.Path.GetFileName(target),
        metadata = new { title = title, details = details, date = dateText }
    });
    for (int i=0; i<clips.Count; i++)
    {
        Variables["sourceClip." + i + ".Name"] = clips[i].Item1;
        Logger.ILog("Clip " + (i+1) + ": " + clips[i].Item1 + " recorded " + clips[i].Item2.ToString("O"));
    }
    int n=0;
    foreach (var item in snapshots)
    {
        Variables["sourceFile." + n + ".Name"] = item.Key;
        Variables["sourceFile." + n + ".Length"] = item.Value.Item1.ToString();
        Variables["sourceFile." + n + ".LastWriteUtcTicks"] = item.Value.Item2.ToString();
        n++;
    }
    Logger.ILog("Complete phone upload verified. Clip order: " + order + ". Scene date: " + dateText);
    return 1;
}
catch (System.Text.Json.JsonException) { Logger.ILog("The manifest is still incomplete; retrying in five minutes."); return 2; }
catch (System.IO.IOException) { Logger.ILog("Upload files are temporarily unavailable; retrying in five minutes."); return 2; }
catch (System.Exception ex) { Logger.ELog(ex.Message); return -1; }
