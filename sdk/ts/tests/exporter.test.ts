import { describe, it, expect } from "vitest";
import { buildOmnevalExporterConfig } from "../src/node/exporter";

describe("buildOmnevalExporterConfig", () => {
  it("sends X-API-Key header when apiKey is provided", () => {
    const config = buildOmnevalExporterConfig({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_abc123",
    });

    expect(config.headers).toBeDefined();
    expect((config.headers as Record<string, string>)["X-API-Key"]).toBe(
      "oev_proj_abc123"
    );
  });

  it("does not send Authorization header", () => {
    const config = buildOmnevalExporterConfig({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_abc123",
    });

    expect((config.headers as Record<string, string>)["Authorization"]).toBeUndefined();
  });

  it("sets headers to undefined when no apiKey is provided", () => {
    const config = buildOmnevalExporterConfig({
      baseUrl: "http://localhost:3000",
    });

    expect(config.headers).toBeUndefined();
  });

  it("points url at {baseUrl}/v1/traces", () => {
    const config = buildOmnevalExporterConfig({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
    });

    expect(config.url).toBe("http://localhost:3000/v1/traces");
  });

  it("strips trailing slash from baseUrl", () => {
    const config = buildOmnevalExporterConfig({
      baseUrl: "http://localhost:3000/",
      apiKey: "oev_proj_test",
    });

    expect(config.url).toBe("http://localhost:3000/v1/traces");
  });
});
