import { getGlobalStartContext } from "@tanstack/react-start";
import { beforeEach, expect, test, vi } from "vitest";
import { render } from "#/test/render";
import { Route } from "../sign-in";

vi.mock("@tanstack/react-start", async (original) => ({
  ...(await original<typeof import("@tanstack/react-start")>()),
  getGlobalStartContext: vi.fn<typeof getGlobalStartContext>(),
}));

const PAGE = { route: Route, path: "/sign-in" };
const URL = "/sign-in?returnTo=%2F";

type Context = NonNullable<ReturnType<typeof getGlobalStartContext>>;

function withContext(context: Omit<Context, "nonce"> | undefined) {
  vi.mocked(getGlobalStartContext).mockReturnValue(
    context === undefined ? undefined : { ...context, nonce: "test" },
  );
}

beforeEach(() => {
  vi.resetAllMocks();
});

test("plain http on another host says why and shows no form", async () => {
  withContext({ configProblems: [], insecureOrigin: true });
  const page = await render(PAGE, { url: URL });
  expect(page.getByTestId("sign-in-insecure")).toBeTruthy();
  expect(page.getByText("Sign-in needs a secure address")).toBeTruthy();
  expect(page.queryByTestId("sign-in-submit")).toBeNull();
  expect(page.queryByLabelText("Password")).toBeNull();
});

test("a secure origin shows the form", async () => {
  withContext({ configProblems: [], insecureOrigin: false });
  const page = await render(PAGE, { url: URL });
  expect(page.getByTestId("sign-in-submit")).toBeTruthy();
  expect(page.queryByTestId("sign-in-insecure")).toBeNull();
});

test("missing configuration is named first, even over plain http", async () => {
  withContext({
    configProblems: [{ variable: "TOGGLESLASH_SESSION_SECRET", minimumLength: 32 }],
    insecureOrigin: true,
  });
  const page = await render(PAGE, { url: URL });
  expect(page.getByTestId("sign-in-unconfigured")).toBeTruthy();
  expect(page.queryByTestId("sign-in-insecure")).toBeNull();
});

test("a client-side navigation, with no request context, shows the form", async () => {
  withContext(undefined);
  const page = await render(PAGE, { url: URL });
  expect(page.getByTestId("sign-in-submit")).toBeTruthy();
  expect(page.queryByTestId("sign-in-insecure")).toBeNull();
});
