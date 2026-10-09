`gif_to_av1_qsv.py` is the GIF conversion helper used by the host and n8n worker
profiles. It produces AV1 in Matroska, verifies the output, preserves source
timestamps and deletes the GIF only when `--delete-source` is explicitly set.
Publication uses a temporary sibling followed by an atomic rename.

NV12 and QSV require even dimensions. The filter pads the right or bottom edge
by one pixel when needed before converting the pixel format. This preserves the
image dimensions within the padded canvas and avoids the Intel encoder crash
seen with odd-width GIFs. Existing even-sized images are unchanged.

Install the helper at the path referenced by the deployed profiles, then update
their asset SHA-256. That changes the worker policy fingerprint. Pending runs
need an explicit compatible policy upgrade before the new worker claims them;
never edit their original admissions or silently replace the recorded hash.
