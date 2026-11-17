import { render as renderDom, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  RouterProvider,
  createMemoryHistory,
  createRootRoute,
  createRouter,
  type AnyRoute,
} from "@tanstack/react-router";
import { isValidElement, type ReactElement } from "react";
import { LinkThroughRouter } from "#/link";

/**
 * Renders a page inside a real TanStack Router on a memory history, so `Link`, `useRouter` and
 * `useNavigate` work, and answers once the router has settled, with Testing Library's queries
 * (unscoped, as `screen`'s), a `userEvent` user and the router.
 *
 * `page` is either an element, shown as the root route's component, or one of the app's file
 * routes with its `path` (as in `routeTree.gen.ts`), mounted under a bare root so its own search
 * validation, loader (the mocked server function) and component run. `url` is where the router starts.
 *
 * A DOM render is a client render: `useHydrated` is true from the first render, so the controls
 * gated on it are enabled. The state before hydration is proved in a browser (`hydration-gate.spec.ts`).
 */
export async function render(
  page: ReactElement | { route: AnyRoute; path: string },
  { url = "/" }: { url?: string } = {},
) {
  const rootRoute = isValidElement(page)
    ? createRootRoute({ component: () => page })
    : createRootRoute();
  if (!isValidElement(page)) {
    // The generated route tree binds a file route to the app's root; bind it to this one instead.
    // What `routeTree.gen.ts` sets with `update`, whose type leaves out these three.
    Object.assign(page.route.options, {
      id: page.path,
      path: page.path,
      getParentRoute: () => rootRoute,
    });
    rootRoute.addChildren([page.route]);
  }
  const router = createRouter({
    routeTree: rootRoute,
    history: createMemoryHistory({ initialEntries: [url] }),
    // The app's own link adapter, so a hackcove link renders as it does in the app.
    InnerWrap: LinkThroughRouter,
  });
  const user = userEvent.setup();
  const view = renderDom(<RouterProvider router={router} />);
  await waitFor(() => {
    if (router.state.status !== "idle") throw new Error("the router is still loading");
  });
  return { ...view, ...screen, user, router };
}

/** A form control's value: a query answers an `HTMLElement`, which has none. */
export function valueOf(element: HTMLElement): string {
  if (
    element instanceof HTMLInputElement ||
    element instanceof HTMLTextAreaElement ||
    element instanceof HTMLSelectElement
  ) {
    return element.value;
  }
  throw new Error(`a ${element.tagName} has no value`);
}

/** Whether a control is disabled, as `:disabled` reads it (a disabled fieldset included). */
export function isDisabled(element: HTMLElement): boolean {
  return element.matches(":disabled");
}
