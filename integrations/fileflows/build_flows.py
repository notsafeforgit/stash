#!/usr/bin/env python3
"""Add manifest support to exported adaptive concat flows; never calls a server."""
import argparse
import copy
import json
from pathlib import Path
from uuid import UUID, uuid5

BASE = Path(__file__).resolve().parent


def replace_once(code, before, after):
    if code.count(before) != 1:
        raise ValueError("The existing flow changed; review its implementation before patching: " + before[:80])
    return code.replace(before, after, 1)


def build(entry, pipeline):
    entry, pipeline = copy.deepcopy(entry), copy.deepcopy(pipeline)
    old_pipeline_id = pipeline["Uid"]
    pipeline["Uid"] = str(uuid5(UUID(old_pipeline_id), "stash-phone-upload-v1"))
    pipeline["Name"] = "Phone uploads - adaptive AV1 pipeline"
    entry["Uid"] = str(uuid5(UUID(entry["Uid"]), "stash-phone-upload-v1"))
    entry["Name"] = "Phone uploads - ordered AV1 and Stash metadata"
    for part in entry["Parts"]:
        if part["FlowElementUid"] == "SubFlow:" + old_pipeline_id:
            part["FlowElementUid"] = "SubFlow:" + pipeline["Uid"]
    prepare = next(p for p in entry["Parts"] if p.get("Name", "").startswith("Prepare exact iPhone"))
    gate_id = str(uuid5(UUID(prepare["Uid"]), "phone-manifest-gate"))
    native_id = str(uuid5(UUID(prepare["Uid"]), "phone-manifest-prepare"))
    retry_id = str(uuid5(UUID(prepare["Uid"]), "phone-upload-retry"))
    for part in entry["Parts"]:
        for connection in part.get("OutputConnections", []):
            if connection["InputNode"] == prepare["Uid"]:
                connection["InputNode"] = gate_id
    native = copy.deepcopy(prepare)
    native.update(Uid=native_id, Name="Verify phone upload and apply chosen clip order", xPos=440, Outputs=2)
    native["Model"]["Code"] = (BASE / "prepare-clips.cs").read_text()
    native["Model"]["Outputs"] = 2
    native["OutputConnections"].append({"Input": 1, "Output": 2, "InputNode": retry_id})
    retry = {"Uid": retry_id, "Name": "Wait for the complete phone upload", "Label": "Reprocess",
             "FlowElementUid": "FileFlows.BasicNodes.Functions.Reprocess", "Type": 2,
             "Inputs": 1, "Outputs": 0, "OutputConnections": [], "xPos": 740, "yPos": 190,
             "Model": {"HoldMinutes": 5}}
    gate = copy.deepcopy(prepare)
    gate.update(Uid=gate_id, Name="Phone manifest present?", yPos=110, Outputs=2)
    gate["Model"] = {"Outputs": 2, "Code": '''string folder = Variables.ContainsKey("folder.Orig.FullName")
    ? Variables["folder.Orig.FullName"].ToString() : Variables["file.FullName"].ToString();
return System.IO.Path.GetFileName(folder).StartsWith("stash-upload-", System.StringComparison.Ordinal)
    || System.IO.File.Exists(System.IO.Path.Combine(folder, "stash-clips.json")) ? 1 : 2;'''}
    gate["OutputConnections"] = [{"Input": 1, "Output": 1, "InputNode": native_id},
                                 {"Input": 1, "Output": 2, "InputNode": prepare["Uid"]}]
    entry["Parts"].extend([gate, native, retry])
    inspect = next(p["Model"] for p in pipeline["Parts"] if p.get("Name", "").startswith("Inspect compatible"))
    inspect["Code"] = replace_once(inspect["Code"], 'else if (string.Equals(sourceMediaKind, "sequential-mp4",',
        '''else if (string.Equals(sourceMediaKind, "ordered-clips", System.StringComparison.OrdinalIgnoreCase))
    clipPattern = new System.Text.RegularExpressions.Regex(@"^clip-[0-9]+\\.(mov|mp4|m4v)$", System.Text.RegularExpressions.RegexOptions.IgnoreCase);
else if (string.Equals(sourceMediaKind, "sequential-mp4",''')
    publish = next(p["Model"] for p in pipeline["Parts"] if p.get("Name", "").startswith("Publish exact output"))
    code = publish["Code"]
    code = replace_once(code, 'bool workingMovedToStage = false;', '''string importPath = getVariable("stashImportPath");
string importText = getVariable("stashImportManifest");
string stagedImport = "";
bool importPublished = false;
if (!string.IsNullOrEmpty(importPath))
{
    if (System.IO.Path.GetDirectoryName(importPath) != targetParent || System.IO.File.Exists(importPath))
        throw new System.Exception("Pending import path changed or already exists.");
    var payload = System.Text.Json.JsonSerializer.Deserialize<System.Collections.Generic.Dictionary<string, object>>(importText);
    payload["size"] = expectedOutputLength;
    importText = System.Text.Json.JsonSerializer.Serialize(payload);
    stagedImport = importPath + "." + publishId + ".part";
}
bool workingMovedToStage = false;''')
    code = replace_once(code, '    if (publishNfo)\n    {\n        System.IO.File.Copy', '''    if (!string.IsNullOrEmpty(stagedImport))
    {
        using (var stream = new System.IO.FileStream(stagedImport, System.IO.FileMode.CreateNew, System.IO.FileAccess.Write))
        {
            var bytes = System.Text.Encoding.UTF8.GetBytes(importText);
            stream.Write(bytes, 0, bytes.Length);
            stream.Flush(true);
        }
    }
    if (publishNfo)
    {
        System.IO.File.Copy''')
    code = replace_once(code, '    try\n    {\n        System.IO.File.Move(stagedVideo, targetVideoPath);', '''    if (!string.IsNullOrEmpty(stagedImport))
    {
        // Publish metadata first; intake waits for the matching complete MKV.
        System.IO.File.Move(stagedImport, importPath);
        importPublished = true;
    }
    try
    {
        System.IO.File.Move(stagedVideo, targetVideoPath);''')
    code = replace_once(code, '        if (publishNfo && nfoPublished && !videoPublished', '''        if (importPublished && !videoPublished && System.IO.File.Exists(importPath))
            System.IO.File.Delete(importPath);
        if (!string.IsNullOrEmpty(stagedImport) && System.IO.File.Exists(stagedImport))
            System.IO.File.Delete(stagedImport);
        if (publishNfo && nfoPublished && !videoPublished''')
    code = replace_once(code, 'Logger.ILog("Deleting only the exact unchanged source snapshot after validated publication.");', '''if (!string.IsNullOrEmpty(importPath) && !importPublished)
{
    Logger.ELog("Pending Stash import was not durably published; original clips are retained.");
    return -1;
}
Logger.ILog("Deleting only the exact unchanged source snapshot after validated publication.");''')
    publish["Code"] = code
    entry["Description"] = entry.get("Description", "") + "\nPhone manifests join clips by recording date or the order chosen in Shortcuts, and publish native Stash metadata."
    return entry, pipeline


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("entry", type=Path)
    parser.add_argument("pipeline", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    entry, pipeline = build(json.loads(args.entry.read_text()), json.loads(args.pipeline.read_text()))
    args.output.mkdir(parents=True, exist_ok=True)
    for name, flow in (("entry", entry), ("pipeline", pipeline)):
        (args.output / (name + ".json")).write_text(json.dumps(flow, indent=2) + "\n")


if __name__ == "__main__":
    main()
