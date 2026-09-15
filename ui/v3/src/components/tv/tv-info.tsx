import { Link } from "@tanstack/react-router";
import { useScenePlayerControls } from "@/components/player/scene-player-controls";
import { Badge } from "@/components/ui/badge";
import { objectTitle } from "@/core/files";
import { useMsg } from "@/hooks/message";
import type { TvScene } from "./use-tv-mutations";

/** Optional metadata stays inside the video layout and owns only its own
 * scrolling area. It never traps focus or suspends TV's playback/navigation. */
export function TvInfo({ scene }: { scene: TvScene }) {
  const msg = useMsg();
  const controls = useScenePlayerControls();
  return (
    <section
      aria-label={msg("tv.action.info", "Information")}
      // biome-ignore lint/a11y/noNoninteractiveTabindex: The bounded metadata scroller needs keyboard focus for native scrolling.
      tabIndex={0}
      data-tv-info
      data-tv-interactive
      className="pointer-events-auto flex max-h-[45%] min-h-0 min-w-0 flex-1 flex-col gap-3 overflow-y-auto overscroll-contain touch-pan-y rounded-sm px-1 py-2 text-white [overflow-wrap:anywhere] [text-shadow:0_1px_3px_rgb(0_0_0/0.95),0_0_8px_rgb(0_0_0/0.65)] focus-visible:outline focus-visible:outline-white/60 sm:max-w-2xl"
    >
      <h2 className="text-lg font-semibold leading-snug sm:text-xl">
        <Link
          to="/scenes/$sceneId"
          params={{ sceneId: scene.id }}
          search={{ t: Math.floor(controls.read().position) }}
          className="hover:underline focus-visible:underline"
        >
          {objectTitle(scene)}
        </Link>
      </h2>
      {scene.studio && (
        <Link
          to="/studios/$studioId"
          params={{ studioId: scene.studio.id }}
          className="w-fit max-w-full text-sm font-medium hover:underline"
        >
          {scene.studio.name}
        </Link>
      )}
      {scene.performers.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {scene.performers.map((performer) => (
            <Link
              key={performer.id}
              to="/performers/$performerId"
              params={{ performerId: performer.id }}
              className="min-w-0 max-w-full text-sm font-medium hover:underline"
            >
              {performer.name}
            </Link>
          ))}
        </div>
      )}
      {scene.details && (
        <p className="whitespace-pre-wrap text-sm leading-relaxed text-white/90">
          {scene.details}
        </p>
      )}
      {scene.tags.length > 0 && (
        <div className="flex flex-wrap gap-1.5">
          {scene.tags.map((tag) => (
            <Link
              key={tag.id}
              to="/tags/$tagId"
              params={{ tagId: tag.id }}
              className="min-w-0 max-w-full"
            >
              <Badge
                variant="outline"
                className="h-auto max-w-full whitespace-normal border-white/30 bg-transparent px-2 py-1 text-left text-white hover:bg-white/10 [overflow-wrap:anywhere]"
              >
                {tag.name}
              </Badge>
            </Link>
          ))}
        </div>
      )}
    </section>
  );
}
