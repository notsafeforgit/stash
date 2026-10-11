import { useCallback, useState } from "react";
import { useIntl } from "react-intl";
import { Link } from "@tanstack/react-router";
import { useMsg } from "@/hooks/message";
import { createScrapeMetricsAPI } from "@/core/native-archive/scrape-metrics-api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardDescription,
  CardContent,
  CardFooter,
} from "@/components/ui/card";
import {
  Table,
  TableHeader,
  TableHead,
  TableRow,
  TableBody,
  TableCell,
} from "@/components/ui/table";
import {
  Collapsible,
  CollapsibleTrigger,
  CollapsibleContent,
} from "@/components/ui/collapsible";
import { Button } from "@/components/ui/button";
import { Spinner } from "@/components/ui/spinner";
import { PageSummary } from "@/components/list/page-summary";
import { ActivityError, ActivityTime, useActivityRead } from "./shared";

export function ScrapeMetrics({ refresh }: { refresh: number }) {
  const msg = useMsg();
  const intl = useIntl();
  const [api] = useState(() => createScrapeMetricsAPI());
  const [retry, setRetry] = useState(0);
  const [offset, setOffset] = useState(0);
  const load = useCallback(
    (signal: AbortSignal) => api(offset, signal),
    [api, offset],
  );
  const read = useActivityRead(String(offset), load, refresh + retry);
  const data = read.current;
  const classes = {
    routine: msg("scrape_metrics.routine", "Routine checks"),
    initial: msg("scrape_metrics.initial", "Initial imports"),
    recovery: msg("scrape_metrics.recovery", "Historical recovery"),
  };
  const operations = {
    listing: msg("scrape_metrics.listing", "Post listings"),
    profile: msg("scrape_metrics.profile", "Profile lookups"),
    "media-resolution": msg(
      "scrape_metrics.resolution",
      "External media resolution",
    ),
    other: msg("scrape_metrics.other", "Other source requests"),
    posts: msg("scrape_metrics.posts", "Captured posts"),
  };
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {msg("scrape_metrics.title", "Scrape request budget")}
        </CardTitle>
        <CardDescription>
          {msg(
            "scrape_metrics.description",
            "Source requests over the last 24 hours, shared by host and n8n workers. Media transfers are separate. Routine checks have priority; counters are retained for 30 days.",
          )}
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {read.busy && <Spinner />}
        {read.error !== undefined && (
          <ActivityError retry={() => setRetry(retry + 1)} />
        )}
        {data && (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>
                    {msg("scrape_metrics.source", "Source and operation")}
                  </TableHead>
                  <TableHead>
                    {msg("scrape_metrics.requests", "Requests")}
                  </TableHead>
                  <TableHead>
                    {msg("scrape_metrics.limited", "Rate limits")}
                  </TableHead>
                  <TableHead>
                    {msg("scrape_metrics.errors", "Errors")}
                  </TableHead>
                  <TableHead>
                    {msg("scrape_metrics.new", "New posts")}
                  </TableHead>
                  <TableHead>
                    {msg("scrape_metrics.repeated", "Repeated posts")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.metrics.map((row) => (
                  <TableRow
                    key={`${row.source_scope}:${row.class}:${row.operation}`}
                  >
                    <TableCell>
                      <div>
                        {row.source_scope.replace(
                          /^(service|host|mirror):/,
                          "",
                        )}
                      </div>
                      <div className="text-muted-foreground">
                        {classes[row.class]} · {operations[row.operation]}
                      </div>
                    </TableCell>
                    <TableCell>{intl.formatNumber(row.requests)}</TableCell>
                    <TableCell>{intl.formatNumber(row.rate_limited)}</TableCell>
                    <TableCell>{intl.formatNumber(row.errors)}</TableCell>
                    <TableCell>{intl.formatNumber(row.new_posts)}</TableCell>
                    <TableCell>
                      {intl.formatNumber(row.repeated_posts)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            {data.metrics.length === 0 && (
              <p>
                {msg(
                  "scrape_metrics.empty",
                  "No source requests recorded yet.",
                )}
              </p>
            )}
            <p className="text-sm text-muted-foreground">
              {msg(
                "scrape_metrics.count_note",
                "Requests count granted attempts; a stopped worker may not send its last grant. Posts count once per run, even when they contain multiple files. Errors include rate limits.",
              )}
            </p>
            <Collapsible>
              <CollapsibleTrigger render={<Button variant="outline" />}>
                {msg("scrape_metrics.checks", "Account checks, oldest first")}
              </CollapsibleTrigger>
              <CollapsibleContent>
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>
                        {msg("scrape_metrics.account", "Account source")}
                      </TableHead>
                      <TableHead>
                        {msg("scrape_metrics.last", "Last completed check")}
                      </TableHead>
                      <TableHead>
                        {msg(
                          "scrape_metrics.overdue",
                          "Overdue beyond 24 hours",
                        )}
                      </TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {data.accounts.map((row) => (
                      <TableRow key={row.collection_uuid}>
                        <TableCell>
                          <Link
                            to="/collections"
                            search={{ collection: row.collection_uuid }}
                          >
                            {row.label}
                          </Link>
                        </TableCell>
                        <TableCell>
                          {row.last_success_ms ? (
                            <ActivityTime
                              value={new Date(
                                row.last_success_ms,
                              ).toISOString()}
                            />
                          ) : (
                            msg("scrape_metrics.never", "No completed check")
                          )}
                        </TableCell>
                        <TableCell>
                          {row.last_success_ms
                            ? intl.formatNumber(row.overdue_seconds / 3600, {
                                style: "unit",
                                unit: "hour",
                                maximumFractionDigits: 1,
                              })
                            : "—"}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <PageSummary
                    total={data.total}
                    page={offset / 50 + 1}
                    pageSize={50}
                  />
                  <div className="flex gap-2">
                    <Button
                      variant="outline"
                      disabled={read.busy || offset === 0}
                      onClick={() => setOffset(Math.max(0, offset - 50))}
                    >
                      {msg("actions.previous", "Previous")}
                    </Button>
                    <Button
                      variant="outline"
                      disabled={read.busy || offset + 50 >= data.total}
                      onClick={() => setOffset(offset + 50)}
                    >
                      {msg("actions.next", "Next")}
                    </Button>
                  </div>
                </div>
              </CollapsibleContent>
            </Collapsible>
          </>
        )}
      </CardContent>
      <CardFooter>
        {msg(
          "scrape_metrics.enrichment",
          "Normal scrapes retain metadata from their original responses. Older posts change only when a scrape encounters them again.",
        )}
      </CardFooter>
    </Card>
  );
}
