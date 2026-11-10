import { Button, Dialog } from "@hackcove/ui-react";
import type { FlagRow } from "#/api/types";

interface ArchiveProps {
  flag: FlagRow;
  onCancel: () => void;
  onConfirm: (flag: FlagRow) => void;
}

export function ArchiveDialog({ flag, onCancel, onConfirm }: ArchiveProps) {
  return (
    <Dialog
      title={`Archive ${flag.name}?`}
      description="An archived flag is hidden from this list and from the SDK ruleset. Its key stays reserved."
      size="sm"
      onClose={onCancel}
      footer={
        <>
          <Button variant="ghost" onClick={onCancel}>
            Cancel
          </Button>
          <Button tone="danger" data-testid="confirm-archive" onClick={() => onConfirm(flag)}>
            Archive
          </Button>
        </>
      }
    />
  );
}
