import {
  createRootRoute,
  createRoute,
  createRouter,
  Link,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { useState } from "react";
import { Button, buttonVariants } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { installRouteTransitions } from "@/core/route-transitions";
import { RouteViewport } from "@/components/layout/route-viewport";
import { getScrollRestorationKey } from "@/core/scroll-restoration";

function TransitionShell() {
  return (
    <div className="flex h-dvh flex-col bg-background text-foreground">
      <header className="flex h-14 shrink-0 items-center border-b px-6">
        Library
      </header>
      <RouteViewport>
        <Outlet />
      </RouteViewport>
    </div>
  );
}

function TransitionList() {
  return (
    <div
      data-scroll-restoration-id="transition-list"
      className="flex-1 overflow-auto p-6"
    >
      <h1>Entities</h1>
      <div className="grid grid-cols-3 gap-4">
        {Array.from({ length: 60 }, (_, index) => (
          <Link<typeof transitionRouter, string, "/transitions/$entityId">
            key={index}
            className={buttonVariants({ variant: "outline" })}
            to="/transitions/$entityId"
            params={{ entityId: String(index + 1) }}
          >
            Entity {index + 1}
          </Link>
        ))}
      </div>
      <Link<typeof transitionRouter, string, "/transitions/$entityId">
        to="/transitions/$entityId"
        params={{ entityId: "slow" }}
      >
        Slow entity
      </Link>
    </div>
  );
}

function TransitionDetail() {
  const { entityId } = detailRoute.useParams();
  const { tab } = detailRoute.useSearch();
  const [count, setCount] = useState(0);
  return (
    <div className="flex flex-1 flex-col gap-4 overflow-auto p-6">
      <h1>Entity {entityId}</h1>
      <div className="flex flex-wrap gap-2">
        <Button
          onClick={() =>
            transitionRouter.navigate({
              to: "/transitions",
              state: { navigationDirection: "back" },
            })
          }
        >
          Back to list
        </Button>
        <Link<typeof transitionRouter, string, "/transitions/$entityId">
          className={buttonVariants({ variant: "outline" })}
          to="/transitions/$entityId"
          params={{ entityId: "2" }}
        >
          Next entity
        </Link>
        <Button
          onClick={() =>
            transitionRouter.navigate({
              to: "/transitions/$entityId",
              params: { entityId },
              search: { tab: "details" },
              hash: "metadata",
            })
          }
        >
          Details tab
        </Button>
        <Button onClick={() => setCount(count + 1)}>Count {count}</Button>
        <Button
          onClick={() =>
            transitionRouter.navigate({
              to: "/transitions/$entityId",
              params: { entityId: "3" },
              replace: true,
            })
          }
        >
          Replace entity
        </Button>
        <Button
          onClick={() =>
            transitionRouter.navigate({
              to: "/transitions/$entityId",
              params: { entityId: "1" },
              state: { navigationDirection: "back" },
            })
          }
        >
          Back to entity
        </Button>
        <Button
          onClick={() =>
            transitionRouter.navigate({
              to: "/transitions/$entityId",
              params: { entityId: "3" },
              state: { routeMotion: false },
            })
          }
        >
          Open without motion
        </Button>
      </div>
      <Input aria-label="Draft" defaultValue="" />
      <p id="metadata">{tab ?? "overview"}</p>
    </div>
  );
}

const rootRoute = createRootRoute({ component: TransitionShell });
const detailRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/transitions/$entityId",
  validateSearch: (search: Record<string, unknown>) => ({
    tab: typeof search.tab === "string" ? search.tab : undefined,
  }),
  loader: async ({ params }) => {
    if (params.entityId === "slow") {
      await new Promise((resolve) => setTimeout(resolve, 300));
    }
  },
  component: TransitionDetail,
});
const transitionRouter = createRouter({
  routeTree: rootRoute.addChildren([
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/transitions",
      component: TransitionList,
    }),
    detailRoute,
  ]),
  basepath: location.pathname.startsWith("/stash/") ? "/stash" : "/",
  scrollRestoration: true,
  getScrollRestorationKey,
});
installRouteTransitions(transitionRouter);

export function RouteTransitionsFixture() {
  return <RouterProvider router={transitionRouter} />;
}
