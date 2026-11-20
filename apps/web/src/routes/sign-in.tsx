import { Button, Callout, CenteredLayout, Field, Input, Stack, Text } from "@hackcove/ui-react";
import { createFileRoute } from "@tanstack/react-router";
import { getGlobalStartContext } from "@tanstack/react-start";
import { safeReturnPath } from "#/api/return-path";

interface SignInSearch {
  returnTo: string;
  failed?: true | undefined;
}

export const Route = createFileRoute("/sign-in")({
  validateSearch: (raw: Record<string, unknown>): SignInSearch => ({
    returnTo: safeReturnPath(raw["returnTo"]),
    failed: raw["failed"] === undefined ? undefined : true,
  }),
  // Whole-page loads reach this page, with the Worker's answer. A client-side navigation has none and
  // shows the form, which is no weaker: the POST is what checks, and it lands back here on a full load.
  loader: () => {
    const ctx = getGlobalStartContext();
    return { problems: ctx?.configProblems ?? [], insecure: ctx?.insecureOrigin ?? false };
  },
  component: SignIn,
});

function SignIn() {
  const { problems, insecure } = Route.useLoaderData();
  const { returnTo, failed } = Route.useSearch();
  if (problems.length > 0) {
    return (
      <CenteredLayout title="Sign-in is not configured">
        <Callout tone="warn" title="Nobody can sign in yet" data-testid="sign-in-unconfigured">
          <Stack gap="xs">
            <Text>
              Set these Worker secrets (in <code>apps/web/.dev.vars</code> when running locally):
            </Text>
            {problems.map((problem) => (
              <Text key={problem.variable}>
                <code>{problem.variable}</code>, at least {problem.minimumLength} characters.
              </Text>
            ))}
          </Stack>
        </Callout>
      </CenteredLayout>
    );
  }
  if (insecure) {
    return (
      <CenteredLayout title="Sign-in needs a secure address">
        <Callout
          tone="warn"
          title="Signing in cannot work over plain http"
          data-testid="sign-in-insecure"
        >
          <Stack gap="xs">
            <Text>
              This page was opened over plain http on a host that is not this machine, so your
              browser would drop the session cookie and send you straight back here.
            </Text>
            <Text>
              Open the dashboard over <code>https</code>, or at <code>localhost</code> on the
              machine that runs it.
            </Text>
          </Stack>
        </Callout>
      </CenteredLayout>
    );
  }
  return (
    <CenteredLayout title="Sign in to ToggleSlash">
      {/* A plain form: the Worker answers the POST with the cookie and a redirect. */}
      <form method="post" action="/sign-in">
        <Stack gap="sm">
          {failed === true && (
            <Callout tone="danger" title="That did not work" data-testid="sign-in-failed">
              <Text>Check the password and try again.</Text>
            </Callout>
          )}
          <input type="hidden" name="returnTo" value={returnTo} />
          <Field label="Password">
            <Input
              name="password"
              type="password"
              autoComplete="current-password"
              required
              autoFocus
            />
          </Field>
          <Button type="submit" data-testid="sign-in-submit">
            Sign in
          </Button>
        </Stack>
      </form>
    </CenteredLayout>
  );
}
