import { Cluster, ClusterGrow, Input, SegmentedControl, Text } from "@hackcove/ui-react";

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
  return (
    <Cluster>
      <SegmentedControl
        label="Flags to show"
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
