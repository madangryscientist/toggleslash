import type { ProjectReady } from "#/api/types";
import { Switchers } from "./Shell";

interface Props {
  loaded: ProjectReady;
  /** The search the page should show after a choice. */
  onChoose: (search: { project?: string | undefined; env?: string | undefined }) => void;
  hideEnvironment?: boolean;
}

/** The project and environment pickers of an operator page; choosing one reloads the page on it. */
export function OperatorSwitchers({ loaded, onChoose, hideEnvironment }: Props) {
  const { project } = loaded;
  return (
    <Switchers
      {...loaded}
      hideEnvironment={hideEnvironment === true}
      onProject={(key) => onChoose({ project: key, env: undefined })}
      onEnvironment={(key) => onChoose({ project: project?.key, env: key })}
    />
  );
}
