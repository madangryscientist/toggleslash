import { AppShell, AppShellBrand, Avatar, ErrorPage, Select } from "@hackcove/ui-react";
import type { ReactNode } from "react";
import type { Environment, Failure, Project } from "#/api/types";

const FAILURE_TITLES: Record<Failure["kind"], string> = {
  unreachable: "The FeatureSlash server is not reachable",
  unconfigured: "The dashboard is not configured",
  unauthorized: "The server rejected the admin token",
  error: "The server could not answer",
};

const FAILURE_HELP: Record<Failure["kind"], string> = {
  unreachable:
    "The dashboard could not get an answer from the server. Check that it is running and that FEATURESLASH_API_URL points at it, then try again.",
  unconfigured:
    "The dashboard reads the server's address and the admin token from its environment. Set both, then try again.",
  unauthorized:
    "The server answered, but not to this token. Check FEATURESLASH_ADMIN_TOKEN against the token the server was started with.",
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
    </>
  );
}

export function Shell({ controls, children }: { controls?: ReactNode; children: ReactNode }) {
  return (
    <AppShell
      brand={
        <AppShellBrand
          logo={<Avatar name="FeatureSlash" size="sm" />}
          label="FeatureSlash"
          href="/"
        />
      }
      controls={controls}
    >
      {children}
    </AppShell>
  );
}
