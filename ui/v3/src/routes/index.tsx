import { createFileRoute } from "@tanstack/react-router";
import { FrontPage } from "@/components/frontpage/front-page";

export const Route = createFileRoute("/")({
  component: FrontPage,
});
