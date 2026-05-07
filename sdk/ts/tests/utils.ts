import { vi } from "vitest";

export function mockFetch(
  handler: (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>
) {
  const fn = vi.fn(handler);
  vi.spyOn(global, "fetch").mockImplementation(fn as any);
  return fn;
}

export function createResponse(
  status: number,
  body?: any
): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 200 ? "OK" : "Error",
    headers: new Headers(),
    json: async () => body,
    text: async () => JSON.stringify(body ?? ""),
    redirected: false,
    type: "basic",
    url: "",
    body: null,
    bodyUsed: false,
    clone: () => createResponse(status, body),
    arrayBuffer: async () => new ArrayBuffer(0),
    blob: async () => new Blob(),
    formData: async () => new FormData(),
    bytes: async () => new Uint8Array(),
  } as Response;
}
