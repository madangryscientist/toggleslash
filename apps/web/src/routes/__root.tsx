import { appShellRootClassFor } from "@hackcove/ui-react";
import { HeadContent, Scripts, createRootRoute, useHydrated } from "@tanstack/react-router";
import type { ReactNode } from "react";
import appCss from "../styles.css?url";

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: "utf-8" },
      { name: "viewport", content: "width=device-width, initial-scale=1" },
      { name: "color-scheme", content: "dark" },
      // The commit this build was made from, so a deploy can be checked against it (docs/dashboard-hosting.md).
      { name: "build-commit", content: APP_BUILD_COMMIT },
      { title: "ToggleSlash" },
    ],
    links: [{ rel: "stylesheet", href: appCss }],
  }),
  shellComponent: RootDocument,
});

function RootDocument({ children }: { children: ReactNode }) {
  // False on the server and through hydration, so the first client render matches the server HTML;
  // the attribute then tells a browser test (or anyone) that the app has taken over the page.
  const hydrated = useHydrated();
  return (
    <html
      lang="en"
      className={appShellRootClassFor("dark")}
      data-hydrated={hydrated ? "" : undefined}
    >
      <head>
        <HeadContent />
      </head>
      <body className="antialiased">
        {children}
        <Scripts />
      </body>
    </html>
  );
}
