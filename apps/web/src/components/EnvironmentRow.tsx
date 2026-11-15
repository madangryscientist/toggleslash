/** The parts of one row of the environments table: the secure-mode switch and the row's buttons. */
import { Button, Cluster, Switch } from "@hackcove/ui-react";
import { useHydrated } from "@tanstack/react-router";
import type { Environment } from "#/api/types";

/** The secure-mode switch: it saves at once, and cannot go on until the environment has a signing key. */
export function SecureModeSwitch({
  env,
  saving,
  onChange,
}: {
  env: Environment;
  saving: boolean;
  onChange: (on: boolean) => void;
}) {
  const hydrated = useHydrated();
  const noKey = env.signingKeyCreatedAt === null;
  return (
    <Switch
      label={env.secureMode ? "On" : "Off"}
      aria-label={`Secure mode for ${env.key}`}
      size="sm"
      checked={env.secureMode}
      busy={saving}
      disabled={!hydrated || noKey}
      description={noKey ? "Make a signing key first." : undefined}
      data-testid={`secure-mode-${env.key}`}
      onChange={onChange}
    />
  );
}

type Act = (environment: Environment) => void;

/** A row's buttons: the signing key, rename and delete, each a dialog. */
export function RowActions({
  env,
  onSigningKey,
  onRename,
  onDelete,
}: {
  env: Environment;
  onSigningKey: Act;
  onRename: Act;
  onDelete: Act;
}) {
  // Disabled until hydrated: before React attaches the handlers a click would do nothing, and nothing would say so.
  const hydrated = useHydrated();
  return (
    <Cluster justify="end">
      <Button
        variant="outline"
        size="sm"
        disabled={!hydrated}
        aria-label={`Signing key for ${env.key}`}
        data-testid={`signing-key-${env.key}`}
        onClick={() => onSigningKey(env)}
      >
        Signing key
      </Button>
      <Button
        variant="outline"
        size="sm"
        disabled={!hydrated}
        aria-label={`Rename ${env.key}`}
        data-testid={`rename-${env.key}`}
        onClick={() => onRename(env)}
      >
        Rename
      </Button>
      <Button
        variant="ghost"
        size="sm"
        tone="danger"
        disabled={!hydrated}
        aria-label={`Delete ${env.key}`}
        data-testid={`delete-${env.key}`}
        onClick={() => onDelete(env)}
      >
        Delete
      </Button>
    </Cluster>
  );
}
