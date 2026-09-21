import { useEffect, useState } from "react";
import { useApolloClient } from "@apollo/client/react";
import { getWSClient } from "@/core/client";
import { observeJobQueue, type QueueJob } from "@/core/job-queue";

export function useJobQueue() {
  const client = useApolloClient();
  const [queue, setQueue] = useState<QueueJob[]>([]);
  useEffect(() => observeJobQueue(client, getWSClient(), setQueue), [client]);
  return queue;
}
