import { useState, useEffect } from "react";

export interface SpriteInfo {
  url: string;
  x: number;
  y: number;
  w: number;
  h: number;
}

/**
 * Fetches and parses a WebVTT sprite sheet file.
 * Returns an array of SpriteInfo entries, one per cue.
 * Uses native fetch + manual VTT parsing — no videojs-vtt.js needed.
 *
 * Expected cue payload format:
 *   https://example.com/sprite.jpg#xywh=0,0,160,90
 */
export function useSpriteInfo(vttUrl: string | undefined): SpriteInfo[] | null {
  const [sprites, setSprites] = useState<SpriteInfo[] | null>(null);

  useEffect(() => {
    if (!vttUrl) {
      setSprites(null);
      return;
    }

    let cancelled = false;

    fetch(vttUrl)
      .then((res) => {
        if (!res.ok) throw new Error(`VTT fetch failed: ${res.status}`);
        return res.text();
      })
      .then((text) => {
        if (cancelled) return;
        const parsed = parseVTT(text, vttUrl);
        setSprites(parsed.length > 0 ? parsed : null);
      })
      .catch(() => {
        if (!cancelled) setSprites(null);
      });

    return () => {
      cancelled = true;
    };
  }, [vttUrl]);

  return sprites;
}

// xywh fragment: #xywh=x,y,w,h
const XYWH_RE = /#xywh=(\d+),(\d+),(\d+),(\d+)$/;

function parseVTT(text: string, baseUrl: string): SpriteInfo[] {
  // Resolve the base for relative URLs — strip the filename from the VTT URL
  const base = baseUrl.replace(/\/[^/]*$/, "/");

  const results: SpriteInfo[] = [];
  // Split into cue blocks (separated by blank lines)
  const blocks = text.split(/\n\s*\n/);

  for (const block of blocks) {
    const lines = block.trim().split("\n");
    // Find the line containing --> (timecode line), then the payload is the next line(s)
    const timecodeIdx = lines.findIndex((l) => l.includes("-->"));
    if (timecodeIdx === -1) continue;

    const payloadLines = lines.slice(timecodeIdx + 1);
    for (const payload of payloadLines) {
      const trimmed = payload.trim();
      if (!trimmed) continue;

      const match = XYWH_RE.exec(trimmed);
      if (!match) continue;

      const rawUrl = trimmed.slice(0, trimmed.length - match[0].length);
      // Resolve relative URLs against the VTT file's directory
      const url = rawUrl.startsWith("http") ? rawUrl : base + rawUrl;

      results.push({
        url,
        x: parseInt(match[1], 10),
        y: parseInt(match[2], 10),
        w: parseInt(match[3], 10),
        h: parseInt(match[4], 10),
      });
    }
  }

  return results;
}
