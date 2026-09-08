import { z } from "zod";
import { PreviewPreset } from "./generated-graphql";
import type * as GQL from "./generated-graphql";

type InputShape<T> = { [Key in keyof T]-?: z.ZodType<T[Key]> };

const generatePreviewOptionsInputSchema = z.looseObject({
  previewExcludeEnd: z.string().nullish().catch(undefined),
  previewExcludeStart: z.string().nullish().catch(undefined),
  previewPreset: z.enum(PreviewPreset).nullish().catch(undefined),
  previewSegmentDuration: z.number().positive().nullish().catch(undefined),
  previewSegments: z.number().int().positive().nullish().catch(undefined),
} satisfies InputShape<GQL.GeneratePreviewOptionsInput>);

const scanMetaDataFilterInputSchema = z.looseObject({
  minModTime: z.string().nullish().catch(undefined),
} satisfies InputShape<GQL.ScanMetaDataFilterInput>);

const scanMetadataInputSchema = z.looseObject({
  filter: scanMetaDataFilterInputSchema.nullish().catch(undefined),
  paths: z.array(z.string()).nullish().catch(undefined),
  rescan: z.boolean().nullish().catch(undefined),
  scanGenerateClipPreviews: z.boolean().nullish().catch(undefined),
  scanGenerateCovers: z.boolean().nullish().catch(undefined),
  scanGenerateImagePhashes: z.boolean().nullish().catch(undefined),
  scanGenerateImagePreviews: z.boolean().nullish().catch(undefined),
  scanGeneratePhashes: z.boolean().nullish().catch(undefined),
  scanGeneratePreviews: z.boolean().nullish().catch(undefined),
  scanGenerateSprites: z.boolean().nullish().catch(undefined),
  scanGenerateThumbnails: z.boolean().nullish().catch(undefined),
} satisfies InputShape<GQL.ScanMetadataInput>);

const autoTagMetadataInputSchema = z.looseObject({
  paths: z.array(z.string()).nullish().catch(undefined),
  performers: z.array(z.string()).nullish().catch(undefined),
  studios: z.array(z.string()).nullish().catch(undefined),
  tags: z.array(z.string()).nullish().catch(undefined),
} satisfies InputShape<GQL.AutoTagMetadataInput>);

const generateMetadataInputSchema = z.looseObject({
  clipPreviews: z.boolean().nullish().catch(undefined),
  covers: z.boolean().nullish().catch(undefined),
  forceTranscodes: z.boolean().nullish().catch(undefined),
  galleryIDs: z.array(z.string()).nullish().catch(undefined),
  imageIDs: z.array(z.string()).nullish().catch(undefined),
  imagePhashes: z.boolean().nullish().catch(undefined),
  imagePreviews: z.boolean().nullish().catch(undefined),
  imageThumbnails: z.boolean().nullish().catch(undefined),
  interactiveHeatmapsSpeeds: z.boolean().nullish().catch(undefined),
  markerIDs: z.array(z.string()).nullish().catch(undefined),
  markerImagePreviews: z.boolean().nullish().catch(undefined),
  markerScreenshots: z.boolean().nullish().catch(undefined),
  markers: z.boolean().nullish().catch(undefined),
  overwrite: z.boolean().nullish().catch(undefined),
  paths: z.array(z.string()).nullish().catch(undefined),
  phashes: z.boolean().nullish().catch(undefined),
  previewOptions: generatePreviewOptionsInputSchema.nullish().catch(undefined),
  previews: z.boolean().nullish().catch(undefined),
  sceneIDs: z.array(z.string()).nullish().catch(undefined),
  sprites: z.boolean().nullish().catch(undefined),
  transcodes: z.boolean().nullish().catch(undefined),
} satisfies InputShape<GQL.GenerateMetadataInput>);

const cleanMetadataInputSchema = z.looseObject({
  dryRun: z.boolean().catch(true),
  ignoreZipFileContents: z.boolean().nullish().catch(undefined),
  paths: z.array(z.string()).nullish().catch(undefined),
} satisfies InputShape<GQL.CleanMetadataInput>);

const cleanGeneratedInputSchema = z.looseObject({
  blobFiles: z.boolean().nullish().catch(undefined),
  dryRun: z.boolean().nullish().catch(undefined),
  imageThumbnails: z.boolean().nullish().catch(undefined),
  markers: z.boolean().nullish().catch(undefined),
  screenshots: z.boolean().nullish().catch(undefined),
  sprites: z.boolean().nullish().catch(undefined),
  transcodes: z.boolean().nullish().catch(undefined),
} satisfies InputShape<GQL.CleanGeneratedInput>);

export const taskDefaultsSchema = z.looseObject({
  scan: scanMetadataInputSchema.optional().catch(undefined),
  autoTag: autoTagMetadataInputSchema.optional().catch(undefined),
  generate: generateMetadataInputSchema.optional().catch(undefined),
  clean: cleanMetadataInputSchema.optional().catch(undefined),
  cleanGenerated: cleanGeneratedInputSchema.optional().catch(undefined),
});
export type TaskDefaults = z.infer<typeof taskDefaultsSchema>;
