import { Cluster, ClusterGrow, Input, SegmentedControl, Text } from "@hackcove/ui-react";
import { useHydrated } from "@tanstack/react-router";

interface Props {
  archived: boolean;
  onView: (archived: boolean) => void;
  query: string;
  onQuery: (query: string) => void;
  /** The count line beside the search. */
  summary: string;
}

/** The Active and Archived choice, the search box and the count. */
export function ListControls({ archived, onView, query, onQuery, summary }: Props) {
  // Disabled until hydrated: before React attaches the handlers a click would do nothing, and nothing would say so.
  const hydrated = useHydrated();
  return (
    <Cluster>
      <SegmentedControl
        label="Flags to show"
        disabled={!hydrated}
        items={[
          { value: "active", label: "Active" },
          { value: "archived", label: "Archived" },
        ]}
        value={archived ? "archived" : "active"}
        onChange={(value) => onView(value === "archived")}
      />
      <ClusterGrow>
        <Input
          type="search"
          aria-label="Search flags"
          disabled={!hydrated}
          placeholder="Search by key, name or tag"
          value={query}
          onChange={(event) => onQuery(event.currentTarget.value)}
        />
      </ClusterGrow>
      <Text tone="muted" numeric>
        {summary}
      </Text>
    </Cluster>
  );
}
