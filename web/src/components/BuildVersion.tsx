import { useEffect, useState } from "react";
import { api, type BuildInfo } from "../api/client";

// The build serving the app, shown quietly at the foot of the sidebar: the
// version, then the short commit and whether it is a dev build. The full
// commit is in the tooltip. The lines stay empty until the server answers,
// or if it can't.
export default function BuildVersion() {
  const [build, setBuild] = useState<BuildInfo | null>(null);

  useEffect(() => {
    let current = true;
    api.version
      .get()
      .then((info) => {
        if (current) setBuild(info);
      })
      .catch(() => {});
    return () => {
      current = false;
    };
  }, []);

  // Two lines tall from the start, so the footer doesn't grow when it loads.
  if (!build) return <div className="h-8" />;
  const details = [build.commit.slice(0, 7), ...(build.dev ? ["dev build"] : [])].join(" · ");
  return (
    <div
      className="h-8 text-[10px] leading-4 text-slate-600"
      title={`Version ${build.version}\nCommit ${build.commit}${build.dev ? "\nDev build" : ""}`}
    >
      <p className="truncate">{build.version}</p>
      <p className="truncate">{details}</p>
    </div>
  );
}
