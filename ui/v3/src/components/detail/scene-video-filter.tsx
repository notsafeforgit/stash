/**
 * Video filter panel for the scene detail view.
 *
 * Manages its own filter state and imperatively applies CSS filter / transform
 * directly to the <video> element inside the scene player.
 * An inline SVG element handles the advanced colour-matrix and gamma filters.
 *
 * The approach mirrors v2.5's SceneVideoFilterPanel but uses shadcn/Base UI
 * Slider components and React-managed SVG instead of imperative DOM creation.
 */
import type React from "react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useIntl } from "react-intl";
import { RotateCcw } from "lucide-react";
import { Slider } from "src/components/ui/slider";
import { Button } from "src/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { cn } from "src/lib/utils";

// ── Types ─────────────────────────────────────────────────────────────────────

export interface SceneVideoFilterFile {
  width?: number | null;
  height?: number | null;
}

export interface SceneVideoFilterProps {
  /** Used only for the Rotate & Scale auto-fit buttons */
  sceneFile?: SceneVideoFilterFile;
}

// ── State shape & defaults ────────────────────────────────────────────────────

type RotateDeg = -180 | -90 | 0 | 90 | 180;

interface Filters {
  // CSS filter
  brightness: number; // 0–200, default 100 → brightness(n%)
  contrast: number; // 0–200, default 100 → contrast(n%)
  saturation: number; // 0–200, default 100 → saturate(n%)
  hueRotate: number; // 0–360, default 0   → hue-rotate(n deg)
  blur: number; // 0–250, default 0   → blur(n/10 px)
  // SVG filter
  gamma: number; // 0–200, default 100 (feComponentTransfer exponent)
  warmth: number; // 0–200, default 100 (feColorMatrix white-balance)
  red: number; // 0–200, default 100 (feColorMatrix channel)
  green: number; // 0–200, default 100
  blue: number; // 0–200, default 100
  // Transform
  rotate: RotateDeg; // locked to 90° multiples
  scale: number; // 50–200, default 100 → scale(n/100)
  aspect: number; // 50–200, default 100 → horizontal stretch factor
}

const DEFAULT_FILTERS: Filters = {
  brightness: 100,
  contrast: 100,
  saturation: 100,
  hueRotate: 0,
  blur: 0,
  gamma: 100,
  warmth: 100,
  red: 100,
  green: 100,
  blue: 100,
  rotate: 0,
  scale: 100,
  aspect: 100,
};

const ROTATE_OPTIONS: RotateDeg[] = [-180, -90, 0, 90, 180];

// ── Sub-components ────────────────────────────────────────────────────────────

function SectionHeader({ children }: { children: React.ReactNode }) {
  return (
    <h3 className="text-[0.75rem] font-semibold uppercase tracking-wider text-muted-foreground mt-4 mb-2 first:mt-0">
      {children}
    </h3>
  );
}

interface FilterRowProps {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  defaultValue: number;
  displayValue: string;
  onChange: (v: number) => void;
}

function FilterRow({
  label,
  value,
  min,
  max,
  step = 1,
  defaultValue,
  displayValue,
  onChange,
}: FilterRowProps) {
  const isDefault = value === defaultValue;
  return (
    <div className="grid grid-cols-[6rem_1fr_auto] items-center gap-2 mb-3 md:mb-1.5">
      <span className="text-xs text-muted-foreground truncate" title={label}>
        {label}
      </span>
      <Slider
        value={value}
        min={min}
        max={max}
        step={step}
        className={cn(
          "[&_[data-slot=slider-thumb]]:size-4 [&_[data-slot=slider-track][data-horizontal]]:h-1.5",
          "md:[&_[data-slot=slider-thumb]]:size-3 md:[&_[data-slot=slider-track][data-horizontal]]:h-1",
        )}
        onValueChange={(v) =>
          onChange(Array.isArray(v) ? (v as number[])[0] : (v as number))
        }
      />
      <Button
        variant="ghost"
        size="xs"
        className={cn(
          "h-auto px-1 text-xs tabular-nums w-12 justify-end font-normal hover:bg-transparent",
          isDefault
            ? "text-muted-foreground"
            : "text-foreground hover:text-primary",
        )}
        title="Click to reset"
        onClick={() => onChange(defaultValue)}
      >
        {displayValue}
      </Button>
    </div>
  );
}

// ── Main component ────────────────────────────────────────────────────────────

export const SceneVideoFilterTab: React.FC<SceneVideoFilterProps> = ({
  sceneFile,
}) => {
  const intl = useIntl();
  const [f, setF] = useState<Filters>(DEFAULT_FILTERS);

  const set = (key: keyof Filters) => (value: number) =>
    setF((prev) => ({ ...prev, [key]: value }));

  // ── Computed SVG filter ──────────────────────────────────────────────────────

  const needsColorMatrix =
    f.warmth !== 100 || f.red !== 100 || f.green !== 100 || f.blue !== 100;
  const needsGamma = f.gamma !== 100;
  const needsSvg = needsColorMatrix || needsGamma;

  // feColorMatrix values (same formula as v2.5)
  const colorMatrixValues = useMemo(() => {
    const wbMV = (f.warmth - 100) / 200; // −0.5 … +0.5
    const rMV = (f.red - 100) / 100;
    const gMV = (f.green - 100) / 100;
    const bMV = (f.blue - 100) / 100;
    const R = 1 + wbMV + rMV;
    const G = 1 + gMV;
    const B = 1 - wbMV + bMV;
    return `${R} 0 0 0 0  0 ${G} 0 0 0  0 0 ${B} 0 0  0 0 0 1 0`;
  }, [f.warmth, f.red, f.green, f.blue]);

  // feComponentTransfer gamma exponent (same formula as v2.5)
  const gammaExp = 1 + (100 - f.gamma) / 200; // 1.5 (dark) … 0.5 (light)

  // ── Computed CSS filter / transform strings ──────────────────────────────────

  const filterStr = useMemo(() => {
    const parts: string[] = [];
    if (needsSvg) parts.push("url(#vjs-video-filter)");
    if (f.brightness !== 100) parts.push(`brightness(${f.brightness}%)`);
    if (f.contrast !== 100) parts.push(`contrast(${f.contrast}%)`);
    if (f.saturation !== 100) parts.push(`saturate(${f.saturation}%)`);
    if (f.hueRotate !== 0) parts.push(`hue-rotate(${f.hueRotate}deg)`);
    if (f.blur > 0) parts.push(`blur(${f.blur / 10}px)`);
    return parts.join(" ");
  }, [f, needsSvg]);

  const transformStr = useMemo(() => {
    const xScale = (f.scale / 100) * (f.aspect / 100);
    const yScale = f.scale / 100;
    const parts: string[] = [];
    if (f.rotate !== 0) parts.push(`rotate(${f.rotate}deg)`);
    if (xScale !== 1 || yScale !== 1) parts.push(`scale(${xScale},${yScale})`);
    return parts.join(" ");
  }, [f.rotate, f.scale, f.aspect]);

  // ── Apply to video element ───────────────────────────────────────────────────

  useEffect(() => {
    const el = document.querySelector(
      "[data-scene-player] video",
    ) as HTMLElement | null;
    if (!el) return;
    el.style.filter = filterStr;
    el.style.transform = transformStr;
    return () => {
      el.style.filter = "";
      el.style.transform = "";
    };
  }, [filterStr, transformStr]);

  // ── Rotate & Scale auto-fit ──────────────────────────────────────────────────

  const handleRotateAndScale = useCallback(
    (direction: "left" | "right") => {
      const newRotate: RotateDeg = direction === "left" ? -90 : 90;
      setF((prev) => ({ ...prev, rotate: newRotate }));

      const sw = sceneFile?.width ?? 1;
      const sh = sceneFile?.height ?? 1;
      const playerEl = document.querySelector(
        "[data-scene-player]",
      ) as HTMLElement | null;
      const pw = playerEl?.clientWidth ?? 1;
      const ph = playerEl?.clientHeight ?? 1;

      const sceneAR = sw / sh;
      let vidW: number, vidH: number;
      if (pw / ph > sceneAR) {
        vidH = ph;
        vidW = (ph / sh) * sw;
      } else {
        vidW = pw;
        vidH = (pw / sw) * sh;
      }

      const rotatedAR = sh / sw;
      let scaleFactor: number;
      if (pw / ph > rotatedAR) {
        scaleFactor = ph / vidW;
      } else {
        scaleFactor = pw / vidH;
      }

      setF((prev) => ({
        ...prev,
        rotate: newRotate,
        scale: Math.round(scaleFactor * 100),
        aspect: 100,
      }));
    },
    [sceneFile],
  );

  // ── Render ───────────────────────────────────────────────────────────────────

  return (
    <div>
      {/* Hidden SVG filter — stays in DOM for url(#vjs-video-filter) reference */}
      {needsSvg && (
        <svg
          xmlns="http://www.w3.org/2000/svg"
          style={{ display: "none" }}
          aria-hidden="true"
        >
          <defs>
            <filter id="vjs-video-filter">
              {needsColorMatrix && (
                <feColorMatrix type="matrix" values={colorMatrixValues} />
              )}
              {needsGamma && (
                <feComponentTransfer>
                  <feFuncR
                    type="gamma"
                    amplitude="1"
                    exponent={gammaExp}
                    offset="0"
                  />
                  <feFuncG
                    type="gamma"
                    amplitude="1"
                    exponent={gammaExp}
                    offset="0"
                  />
                  <feFuncB
                    type="gamma"
                    amplitude="1"
                    exponent={gammaExp}
                    offset="0"
                  />
                  <feFuncA type="gamma" amplitude="1" exponent="1" offset="0" />
                </feComponentTransfer>
              )}
            </filter>
          </defs>
        </svg>
      )}

      {/* ── Color ──────────────────────────────────────────────────────────── */}
      <SectionHeader>
        {intl.formatMessage({
          id: "effect_filters.name",
          defaultMessage: "Color",
        })}
      </SectionHeader>

      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.brightness",
          defaultMessage: "Brightness",
        })}
        value={f.brightness}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.brightness}%`}
        onChange={set("brightness")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.contrast",
          defaultMessage: "Contrast",
        })}
        value={f.contrast}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.contrast}%`}
        onChange={set("contrast")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.saturation",
          defaultMessage: "Saturation",
        })}
        value={f.saturation}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.saturation}%`}
        onChange={set("saturation")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.hue",
          defaultMessage: "Hue",
        })}
        value={f.hueRotate}
        min={0}
        max={360}
        defaultValue={0}
        displayValue={`${f.hueRotate}°`}
        onChange={set("hueRotate")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.blur",
          defaultMessage: "Blur",
        })}
        value={f.blur}
        min={0}
        max={250}
        defaultValue={0}
        displayValue={`${f.blur / 10}px`}
        onChange={set("blur")}
      />

      {/* ── Advanced ───────────────────────────────────────────────────────── */}
      <SectionHeader>
        {intl.formatMessage({
          id: "effect_filters.advanced",
          defaultMessage: "Advanced",
        })}
      </SectionHeader>

      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.gamma",
          defaultMessage: "Gamma",
        })}
        value={f.gamma}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.gamma}`}
        onChange={set("gamma")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.warmth",
          defaultMessage: "Warmth",
        })}
        value={f.warmth}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.warmth}`}
        onChange={set("warmth")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.red",
          defaultMessage: "Red",
        })}
        value={f.red}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.red}%`}
        onChange={set("red")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.green",
          defaultMessage: "Green",
        })}
        value={f.green}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.green}%`}
        onChange={set("green")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.blue",
          defaultMessage: "Blue",
        })}
        value={f.blue}
        min={0}
        max={200}
        defaultValue={100}
        displayValue={`${f.blue}%`}
        onChange={set("blue")}
      />

      {/* ── Geometry ───────────────────────────────────────────────────────── */}
      <SectionHeader>
        {intl.formatMessage({
          id: "effect_filters.name_transforms",
          defaultMessage: "Geometry",
        })}
      </SectionHeader>

      {/* Rotate — select locked to 90° multiples */}
      <div className="grid grid-cols-[6rem_1fr] items-center gap-2 mb-1.5">
        <span className="text-xs text-muted-foreground">
          {intl.formatMessage({
            id: "effect_filters.rotate",
            defaultMessage: "Rotate",
          })}
        </span>
        <Select
          value={String(f.rotate)}
          onValueChange={(v) =>
            setF((prev) => ({ ...prev, rotate: Number(v) as RotateDeg }))
          }
        >
          <SelectTrigger size="sm" className="w-full">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {ROTATE_OPTIONS.map((deg) => (
              <SelectItem key={deg} value={String(deg)}>
                {deg}°
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.scale",
          defaultMessage: "Scale",
        })}
        value={f.scale}
        min={50}
        max={200}
        defaultValue={100}
        displayValue={`${f.scale}%`}
        onChange={set("scale")}
      />
      <FilterRow
        label={intl.formatMessage({
          id: "effect_filters.aspect",
          defaultMessage: "Aspect",
        })}
        value={f.aspect}
        min={50}
        max={200}
        defaultValue={100}
        displayValue={`${f.aspect}%`}
        onChange={set("aspect")}
      />

      {/* ── Actions ────────────────────────────────────────────────────────── */}
      <div className="mt-4 flex flex-col gap-2">
        <div className="grid grid-cols-2 gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleRotateAndScale("left")}
          >
            {intl.formatMessage({
              id: "effect_filters.rotate_left_and_scale",
              defaultMessage: "↺ Rotate Left & Fit",
            })}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => handleRotateAndScale("right")}
          >
            {intl.formatMessage({
              id: "effect_filters.rotate_right_and_scale",
              defaultMessage: "↻ Rotate Right & Fit",
            })}
          </Button>
        </div>
        <Button
          variant="ghost"
          size="sm"
          className="text-muted-foreground"
          onClick={() => setF(DEFAULT_FILTERS)}
        >
          <RotateCcw size={13} className="mr-1" />
          {intl.formatMessage({
            id: "effect_filters.reset_all",
            defaultMessage: "Reset all",
          })}
        </Button>
      </div>
    </div>
  );
};
