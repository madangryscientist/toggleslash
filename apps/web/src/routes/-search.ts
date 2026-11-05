/** Search params the operator routes share. A leading `-` keeps this file out of the route tree. */
export interface ProjectSearch {
  project?: string | undefined;
  env?: string | undefined;
}

export function param(value: unknown): string | undefined {
  return typeof value === "string" && value !== "" ? value : undefined;
}
