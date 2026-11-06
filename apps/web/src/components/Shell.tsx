import {
  AppShell,
  AppShellBrand,
  Avatar,
  Button,
  ErrorPage,
  Select,
  Tabs,
} from "@hackcove/ui-react";
import { Link } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { signOut } from "#/api/session";
import type { Environment, Failure, Project } from "#/api/types";

const FAILURE_TITLES: Record<Failure["kind"], string> = {
  unreachable: "The ToggleSlash server is not reachable",
  unconfigured: "The dashboard is not configured",
  unauthorized: "The server rejected the admin token",
  error: "The server could not answer",
};

const FAILURE_HELP: Record<Failure["kind"], string> = {
  unreachable:
    "The dashboard could not get an answer from the server. Check that it is running and that TOGGLESLASH_API_URL points at it, then try again.",
  unconfigured:
    "The dashboard reads the server's address and the admin token from its environment. Set both, then try again.",
  unauthorized:
    "The server answered, but not to this token. Check TOGGLESLASH_ADMIN_TOKEN against the token the server was started with.",
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
  /** The audit log filters by environment itself, so it has no environment picker. */
  hideEnvironment?: boolean;
  onProject: (key: string) => void;
  onEnvironment: (key: string) => void;
}

/** The project and environment pickers in the shell's header. */
export function Switchers(props: SwitcherProps) {
  const { projects, project, environments, environment } = props;
  return (
    <>
      <Select
        aria-label="Project"
        width="auto"
        value={project?.key ?? ""}
        disabled={props.projectLocked === true || projects.length === 0}
        onChange={(event) => props.onProject(event.currentTarget.value)}
      >
        {projects.length === 0 && <option value="">No projects</option>}
        {projects.map((p) => (
          <option key={p.key} value={p.key}>
            {p.name}
          </option>
        ))}
      </Select>
      {props.hideEnvironment !== true && (
        <Select
          aria-label="Environment"
          width="auto"
          value={environment?.key ?? ""}
          disabled={environments.length === 0}
          onChange={(event) => props.onEnvironment(event.currentTarget.value)}
        >
          {environments.length === 0 && <option value="">No environments</option>}
          {environments.map((e) => (
            <option key={e.key} value={e.key}>
              {e.name}
            </option>
          ))}
        </Select>
      )}
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
      variant="pill"
      size="sm"
      label="Sections"
      items={[
        {
          label: "Flags",
          active: active === "flags",
          render: (p) => <Link to="/" search={scope} {...p} data-testid="nav-flags" />,
        },
        {
          label: "Environments",
          active: active === "environments",
          render: (p) => (
            <Link to="/environments" search={scope} {...p} data-testid="nav-environments" />
          ),
        },
        {
          label: "SDK keys",
          active: active === "keys",
          render: (p) => <Link to="/keys" search={scope} {...p} data-testid="nav-keys" />,
        },
        {
          label: "Segments",
          active: active === "segments",
          render: (p) => <Link to="/segments" search={scope} {...p} data-testid="nav-segments" />,
        },
        {
          label: "Audit log",
          active: active === "audit",
          render: (p) => <Link to="/audit" search={scope} {...p} data-testid="nav-audit" />,
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
  return (
    <Button variant="outline" size="sm" data-testid="sign-out" onClick={() => void leave()}>
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
    <AppShell
      brand={
        <AppShellBrand
          logo={<Avatar name="ToggleSlash" size="sm" />}
          label="ToggleSlash"
          href="/"
        />
      }
      nav={<Nav {...nav} />}
      controls={
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
