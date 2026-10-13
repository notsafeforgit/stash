// Run with prepare-clips.cs wrapped in Func<int> prepare and an empty testRoot.
// This exercises the actual FileFlows C# compiler and file validation path.
if (System.IO.Directory.GetFileSystemEntries(testRoot).Length != 0)
    throw new System.Exception("Preparation tests require an empty, dedicated folder");
foreach (string kind in new[] { "blank", "stem", "full", "ambiguous", "missing", "checksum",
    "traversal", "collision", "unicode", "ordered", "location", "location-checksum",
    "location-ambiguous", "location-missing", "unsupported", "location-ordered" })
{
    string batch;
    using (var sha = System.Security.Cryptography.SHA256.Create())
        batch = System.Convert.ToHexString(sha.ComputeHash(System.Text.Encoding.UTF8.GetBytes(kind))).ToLowerInvariant();
    string folder = System.IO.Path.Combine(testRoot, "stash-upload-" + batch + ".part");
    System.IO.Directory.CreateDirectory(folder);
    bool location = kind.StartsWith("location", System.StringComparison.Ordinal);
    bool ordered = kind == "ordered" || kind == "location-ordered";
    bool ambiguous = kind == "ambiguous" || kind == "location-ambiguous";
    bool missing = kind == "missing" || kind == "location-missing";
    bool checksum = kind == "checksum" || kind == "location-checksum";
    string name = kind == "stem" ? "clip-1.MP4" : missing ? "clip-2.mov" : "clip-1.mov";
    byte[] content = System.Text.Encoding.UTF8.GetBytes("Synthetic uploaded clip");
    System.IO.File.WriteAllBytes(System.IO.Path.Combine(folder, name), content);
    string digest;
    using (var sha = System.Security.Cryptography.SHA256.Create())
        digest = System.Convert.ToHexString(sha.ComputeHash(content)).ToLowerInvariant();
    if (ambiguous) System.IO.File.WriteAllBytes(System.IO.Path.Combine(folder, "clip-1.mp4"), content);
    var entries = new System.Collections.Generic.List<object>();
    string suppliedName = kind == "stem" ? "clip-1" : kind == "full" ? name :
        kind == "traversal" ? "../clip-1.mov" : kind == "unsupported" ? "clip-1.txt" :
        location ? "On My iPhone" : "";
    entries.Add(new { name = suppliedName, recorded_at = "2026-10-11T17:00:00-07:00",
        sha256 = checksum ? new string('0', 64) : digest });
    if (ordered)
    {
        byte[] secondContent = System.Text.Encoding.UTF8.GetBytes("A different synthetic clip");
        System.IO.File.WriteAllBytes(System.IO.Path.Combine(folder, "clip-2.mov"), secondContent);
        string secondDigest;
        using (var sha = System.Security.Cryptography.SHA256.Create())
            secondDigest = System.Convert.ToHexString(sha.ComputeHash(secondContent)).ToLowerInvariant();
        entries.Add(new { name = location ? "On My iPhone" : "", recorded_at = "2026-10-11T10:00:00-07:00", sha256 = secondDigest });
    }
    string title = kind == "unicode" ? string.Concat(System.Linq.Enumerable.Repeat("😀", 90)) : kind;
    if (kind == "collision")
        System.IO.File.WriteAllText(System.IO.Path.Combine(testRoot, title + " [" + batch.Substring(0, 8) + "].mkv"), "Existing video");
    object clips = ordered ? (object)entries : entries[0];
    int expectedResult = ambiguous || kind == "traversal" || kind == "unsupported" || kind == "collision" ? -1 :
        missing || checksum ? 2 : 1;
    System.IO.File.WriteAllText(System.IO.Path.Combine(folder, "stash-clips.json"),
        System.Text.Json.JsonSerializer.Serialize(new {
            format = "stash-clip-batch-v1", batch_id = batch, title = title, details = "Test description",
            date = "", order = "recorded_at", clips = clips, expected_result = expectedResult,
            expected_order = ordered ? "clip-2.mov,clip-1.mov" : name
        }));
}
int prepareChecks = 0;
foreach (string folder in System.IO.Directory.GetDirectories(testRoot))
{
    string manifest = System.IO.Path.Combine(folder, "stash-clips.json");
    string original = System.IO.File.ReadAllText(manifest);
    using var expected = System.Text.Json.JsonDocument.Parse(original);
    var root = expected.RootElement;
    Variables["folder.Orig.FullName"] = folder;
    int result = prepare();
    if (result != root.GetProperty("expected_result").GetInt32())
        throw new System.Exception("Unexpected preparation result for " + folder + ": " + result);
    if (System.IO.File.ReadAllText(manifest) != original)
        throw new System.Exception("Preparation rewrote an uploaded manifest");
    if (result == 1)
    {
        var names = new System.Collections.Generic.List<string>();
        int count = int.Parse(Variables["sourceClipCount"].ToString());
        for (int i = 0; i < count; i++) names.Add(Variables["sourceClip." + i + ".Name"].ToString());
        if (string.Join(",", names) != root.GetProperty("expected_order").GetString())
            throw new System.Exception("Incorrect resolved clip order");
        using var handoff = System.Text.Json.JsonDocument.Parse(Variables["stashImportManifest"].ToString());
        string title = handoff.RootElement.GetProperty("metadata").GetProperty("title").GetString();
        if (title != root.GetProperty("title").GetString()) throw new System.Exception("The metadata title changed");
        string basename = Variables["targetBaseName"].ToString();
        string batch = root.GetProperty("batch_id").GetString();
        if (!basename.EndsWith(" [" + batch.Substring(0, 8) + "]") || basename.Contains(batch))
            throw new System.Exception("Output still has the full batch ID");
        // The pipeline's atomic publication name must fit a 255-byte component.
        string staging = "." + basename + "." + new string('a', 32) + ".partial.mkv";
        if (new System.Text.UTF8Encoding(false, true).GetByteCount(staging) > 255)
            throw new System.Exception("Output staging name is too long");
    }
    prepareChecks++;
}
Logger.ILog("Phone upload preparation checks passed: " + prepareChecks);
System.IO.File.WriteAllText(System.IO.Path.Combine(testRoot, "prepare-proof.json"),
    System.Text.Json.JsonSerializer.Serialize(new {passed = prepareChecks}));
return 1;
