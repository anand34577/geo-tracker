import { useEffect } from "react";
import { queryClient, type Point } from "./api";

/** Subscribes to server events for the signed-in user and refreshes cached data. */
export function useLiveUpdates() {
  useEffect(() => {
    const es = new EventSource("/api/v1/live");
    es.addEventListener("point", (e) => {
      const { point } = JSON.parse((e as MessageEvent).data) as { point: Point };
      queryClient.setQueryData(["latest"], point);
      queryClient.invalidateQueries({ queryKey: ["points"] });
      queryClient.invalidateQueries({ queryKey: ["devices"] });
    });
    es.addEventListener("timeline", () => {
      queryClient.invalidateQueries({ queryKey: ["timeline"] });
      queryClient.invalidateQueries({ queryKey: ["places"] });
    });
    es.addEventListener("family", () => {
      queryClient.invalidateQueries({ queryKey: ["family"] });
      queryClient.invalidateQueries({ queryKey: ["groups"] });
    });
    es.addEventListener("import", () => {
      queryClient.invalidateQueries({ queryKey: ["imports"] });
      queryClient.invalidateQueries({ queryKey: ["stats"] });
    });
    return () => es.close();
  }, []);
}
