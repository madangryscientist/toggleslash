import {
  AppShell,
  AppShellBrand,
  Avatar,
  Button,
  ErrorPage,
  Select,
  Tabs,
} from "@hackcove/ui-react";
import { useHydrated } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { signOut } from "#/api/session";
import type { Environment, Failure, Project } from "#/api/types";
import { appHref } from "./href";

const FAILURE_TITLES: Record<Failure["kind"], string> = {
  unreachable: "The ToggleSlash server is not reachable",
  unconfigured: "The dashboard is not configured",
  unauthorized: "The admin API refused the dashboard",
  error: "The server could not answer",
};

const FAILURE_HELP: Record<Failure["kind"], string> = {
  unreachable:
    "The dashboard could not get an answer from the server. Check that it is running and that TOGGLESLASH_API_URL points at it, then try again.",
  unconfigured:
    "The dashboard reads the server's address and the admin token from its environment, and behind Cloudflare Access both TOGGLESLASH_ACCESS_CLIENT_ID and TOGGLESLASH_ACCESS_CLIENT_SECRET. Set them, then try again.",
  unauthorized:
    "The admin API answered, but not to these credentials. Check TOGGLESLASH_ADMIN_TOKEN against the token the server was started with and, behind Cloudflare Access, TOGGLESLASH_ACCESS_CLIENT_ID and TOGGLESLASH_ACCESS_CLIENT_SECRET against the Access service token.",
  error: "The server answered with an error. Try again; if it persists, look at the server's log.",
};

/** The page every route shows when the server cannot be used. */
export function ServerDown({ failure, onRetry }: { failure: Failure; onRetry: () => void }) {
  return (
    <ErrorPage
      data-testid="server-down"
      title={FAILURE_TITLES[failure.kind]}
      detail={failure.message}
      onRetry={onRetry}
    >
      {FAILURE_HELP[failure.kind]}
    </ErrorPage>
  );
}

interface SwitcherProps {
  projects: Project[];
  project: Project | null;
  environments: Environment[];
  environment: Environment | null;
  /** A page about one project's flag fixes the project: the picker shows it and is disabled. */
  projectLocked?: boolean;
  /** The audit trail filters by environment itself, so it has no environment picker. */
  hideEnvironment?: boolean;
  onProject: (key: string) => void;
  onEnvironment: (key: string) => void;
}

/** The project picker. Disabled until hydrated: before React attaches the handlers a change would do nothing. */
function ProjectSelect(props: SwitcherProps) {
  const { projects, project } = props;
  const hydrated = useHydrated();
  return (
    <Select
      aria-label="Project"
      width="auto"
      value={project?.key ?? ""}
      disabled={!hydrated || props.projectLocked === true || projects.length === 0}
      onChange={(event) => props.onProject(event.currentTarget.value)}
      items={
        projects.length === 0
          ? [{ value: "", label: "No projects" }]
          : projects.map((p) => ({ value: p.key, label: p.name }))
      }
    />
  );
}

/** The environment picker. Disabled until hydrated, as the project picker is. */
function EnvironmentSelect(props: SwitcherProps) {
  const { environments, environment } = props;
  const hydrated = useHydrated();
  return (
    <Select
      aria-label="Environment"
      width="auto"
      value={environment?.key ?? ""}
      disabled={!hydrated || environments.length === 0}
      onChange={(event) => props.onEnvironment(event.currentTarget.value)}
      items={
        environments.length === 0
          ? [{ value: "", label: "No environments" }]
          : environments.map((e) => ({ value: e.key, label: e.name }))
      }
    />
  );
}

/** The project and environment pickers in the shell's header. */
export function Switchers(props: SwitcherProps) {
  return (
    <>
      <ProjectSelect {...props} />
      {props.hideEnvironment !== true && <EnvironmentSelect {...props} />}
    </>
  );
}

type Page = "flags" | "environments" | "keys" | "segments" | "audit";

interface NavScope {
  /** Which page is showing, so its tab is current. */
  active?: Page | undefined;
  /** The project and environment the other pages open on. */
  project?: string | undefined;
  env?: string | undefined;
}

/** The dashboard's sections, each a link that carries the project and environment along. */
function Nav({ active, project, env }: NavScope) {
  const scope = { project, env };
  return (
    <Tabs
      label="Sections"
      items={[
        {
          label: "Flags",
          selected: active === "flags",
          href: appHref("/", scope),
          "data-testid": "nav-flags",
        },
        {
          label: "Environments",
          selected: active === "environments",
          href: appHref("/environments", scope),
          "data-testid": "nav-environments",
        },
        {
          label: "SDK keys",
          selected: active === "keys",
          href: appHref("/keys", scope),
          "data-testid": "nav-keys",
        },
        {
          label: "Segments",
          selected: active === "segments",
          href: appHref("/segments", scope),
          "data-testid": "nav-segments",
        },
        {
          label: "Audit trail",
          selected: active === "audit",
          href: appHref("/audit", scope),
          "data-testid": "nav-audit",
        },
      ]}
    />
  );
}

/** A whole-page load afterwards, so nothing of the signed-in page stays in memory. */
async function leave(): Promise<void> {
  await signOut();
  window.location.assign("/sign-in");
}

function SignOut() {
  // Disabled until hydrated: before React attaches the handlers a click would do nothing, and nothing would say so.
  const hydrated = useHydrated();
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={!hydrated}
      data-testid="sign-out"
      onClick={() => void leave()}
    >
      Sign out
    </Button>
  );
}

interface ShellProps extends NavScope {
  controls?: ReactNode;
  children: ReactNode;
}

export function Shell({ controls, children, ...nav }: ShellProps) {
  return (
    // Compact: the dashboard is tables and forms, and running text matches the cells' size. A text
    // field is still 16px on a touch screen, so iOS does not zoom on focus.
    <AppShell
      density="compact"
      brand={
        <AppShellBrand
          logo={<Avatar name="ToggleSlash" size="sm" />}
          label="ToggleSlash"
          href="/"
        />
      }
      nav={<Nav {...nav} />}
      menuControls={
        <>
          {controls}
          <SignOut />
        </>
      }
    >
      {children}
    </AppShell>
  );
}
