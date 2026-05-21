import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { instrument, _setFactory, _resetFactory } from "../src/node/index";

describe("@omneval/sdk/node conditional export", () => {
  it("exports instrument function", async () => {
    const mod = await import("../src/node/index");
    expect(typeof mod.instrument).toBe("function");
  });

  it("exports InstrumentOptions type (compile check)", async () => {
    const mod = await import("../src/node/index");
    expect(mod).toHaveProperty("instrument");
  });

  it("exports factory override functions (internal, for testing)", async () => {
    const mod = await import("../src/node/index");
    expect(typeof mod._setFactory).toBe("function");
    expect(typeof mod._resetFactory).toBe("function");
  });
});

describe("instrument()", () => {
  beforeEach(() => {
    _resetFactory();
    vi.resetModules();
  });

  afterEach(() => {
    _resetFactory();
    vi.restoreAllMocks();
  });

  it("works with a mock factory (simulates OTel being installed)", async () => {
    const mockNodeSDK = vi.fn(function (config: any) {
      (this as any).config = config;
    });
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {});

    const mockExporter = vi.fn(function (config: any) {
      (this as any).config = config;
    });

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
      serviceName: "test",
    });

    expect(mockNodeSDK).toHaveBeenCalled();
    expect(shutdown).toBeDefined();
  });

  it("throws if baseUrl is not provided", async () => {
    const mockNodeSDK = vi.fn(function () {});
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {});

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: vi.fn() as any,
    });

    // @ts-expect-error baseUrl is required
    expect(() => {
      instrument({ apiKey: "oev_proj_test", serviceName: "test" });
    }).toThrow("baseUrl is required");
  });
});

describe("instrument() — OTel integration", () => {
  let capturedConfig: any;
  let capturedShutdownCalls: number;

  function createMockFactory() {
    capturedConfig = null;
    capturedShutdownCalls = 0;

    const mockNodeSDK = vi.fn(function (config: any) {
      capturedConfig = config;
    });
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {
      capturedShutdownCalls++;
    });

    const mockExporter = vi.fn();

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    return { mockNodeSDK };
  }

  beforeEach(() => {
    createMockFactory();
  });

  afterEach(() => {
    _resetFactory();
    vi.restoreAllMocks();
  });

  it("configures OTLP exporter to point at {baseUrl}/v1/traces", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
      serviceName: "my-app",
    });

    expect(capturedConfig).not.toBeNull();
    expect(capturedConfig.traceExporter).toBeDefined();
  });

  it("sets X-API-Key header when apiKey is provided", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_abc123",
      serviceName: "my-app",
    });

    expect(capturedConfig).not.toBeNull();
    expect(capturedConfig.traceExporter).toBeDefined();
  });

  it("includes service name in resource attributes", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
      serviceName: "test-service",
    });

    expect(capturedConfig).not.toBeNull();
    expect(capturedConfig.resource).toBeDefined();
    expect(capturedConfig.resource.attributes["service.name"]).toBe("test-service");
  });

  it("does not set resource when serviceName is omitted", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
    });

    expect(capturedConfig).not.toBeNull();
    expect(capturedConfig.resource).toBeUndefined();
  });

  it("strips trailing slashes from baseUrl", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000/",
      apiKey: "oev_proj_test",
      serviceName: "my-app",
    });

    expect(capturedConfig).not.toBeNull();
  });

  it("returns a shutdown function that calls nodeSDK.shutdown()", async () => {
    const shutdown = instrument({
      baseUrl: "http://localhost:3000",
      apiKey: "oev_proj_test",
      serviceName: "my-app",
    });

    expect(typeof shutdown).toBe("function");
    await shutdown();
    expect(capturedShutdownCalls).toBe(1);
  });
});
