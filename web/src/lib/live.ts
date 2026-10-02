import { useEffect } from "react";
import { queryClient, type Point } from "./api";

/** Subscribes to server events for the signed-in user and refreshes cached data. */
export function useLiveUpdates() {
  useEffect(() => {
    const es = new EventSource("/api/v1/live");
    es.addEventListener("point", (e) => {
      const { point } = JSON.parse((e as MessageEvent).data) as { point: Point };
      queryClient.setQueryData(["latest"], point);
      // Only ranges that include the new fix (["points", from, to, user]); history views stay cached.
      queryClient.invalidateQueries({ queryKey: ["points"], predicate: (q) => !q.queryKey[3] && Number(q.queryKey[2]) >= point.ts });
      queryClient.invalidateQueries({ queryKey: ["devices"] });
      queryClient.invalidateQueries({ queryKey: ["stats"] });
    });
    es.addEventListener("timeline", (e) => {
      const { from } = JSON.parse((e as MessageEvent).data) as { from: number };
      // Rebuilt from `from` onward: only own timeline ranges ending after it change.
      queryClient.invalidateQueries({ queryKey: ["timeline"], predicate: (q) => !q.queryKey[3] && Number(q.queryKey[2]) >= from });
      queryClient.invalidateQueries({ queryKey: ["places"] });
      queryClient.invalidateQueries({ queryKey: ["insights"] });
    });
    es.addEventListener("family", () => {
      queryClient.invalidateQueries({ queryKey: ["family"] });
      queryClient.invalidateQueries({ queryKey: ["groups"] });
    });
    es.addEventListener("export", () => queryClient.invalidateQueries({ queryKey: ["exports"] }));
    es.addEventListener("import", () => {
      queryClient.invalidateQueries({ queryKey: ["imports"] });
      queryClient.invalidateQueries({ queryKey: ["stats"] });
    });
    return () => es.close();
  }, []);
}
