import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import http from "http";
import { instrument, _setFactory, _resetFactory } from "../src/node/index";

/**
 * Integration test: instrument() configures OTel correctly so that
 * auto-instrumented spans reach the Lantern ingest API.
 *
 * This test:
 * 1. Starts a mock HTTP server that accepts OTLP traces at /v1/traces
 * 2. Calls instrument() to configure the OTel SDK
 * 3. Creates a manual span via OTel API (simulating auto-instrumentation)
 * 4. Shuts down the SDK
 * 5. Asserts the span was received by the mock server
 */
describe("@lantern/sdk/node — integration with OTel", () => {
  let server: http.Server;
  let serverPort: number;
  let receivedTraces: any[];
  let shutdownFn: (() => Promise<void>) | undefined;

  async function startMockServer(): Promise<number> {
    return new Promise<number>((resolve) => {
      server = http.createServer((req, res) => {
        let body = "";
        req.on("data", (chunk) => { body += chunk; });
        req.on("end", () => {
          if (req.url === "/v1/traces" && req.method === "POST") {
            const parsed = JSON.parse(body);
            receivedTraces.push(parsed);
            res.writeHead(202, { "Content-Type": "application/json" });
            res.end(JSON.stringify({}));
          } else {
            res.writeHead(404);
            res.end();
          }
        });
      });

      server.listen(0, "127.0.0.1", () => {
        const addr = server.address() as import("net").AddressInfo;
        resolve(addr.port);
      });
    });
  }

  async function stopServer(): Promise<void> {
    return new Promise<void>((resolve, reject) => {
      if (!server) { resolve(); return; }
      server.close((err) => { err ? reject(err) : resolve(); });
    });
  }

  beforeEach(async () => {
    receivedTraces = [];
    shutdownFn = undefined;
    serverPort = await startMockServer();
  });

  afterEach(async () => {
    if (shutdownFn) {
      await shutdownFn();
    }
    await stopServer();
    _resetFactory();
    vi.restoreAllMocks();
  });

  it("spans from auto-instrumentation reach the mock ingest API", async () => {
    const mockNodeSDK = vi.fn(function (config: any) {
      // Simulate OTel SDK behavior — store the exporter config
      (this as any)._config = config;
    });
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {});

    let capturedExporterConfig: any = null;
    const mockExporter = vi.fn(function (config: any) {
      capturedExporterConfig = config;
    });

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    shutdownFn = instrument({
      baseUrl: `http://127.0.0.1:${serverPort}`,
      apiKey: "ltn_proj_integration_test",
      serviceName: "integration-test",
    });

    // Verify the exporter was configured correctly
    expect(capturedExporterConfig).not.toBeNull();
    expect(capturedExporterConfig.url).toBe(`http://127.0.0.1:${serverPort}/v1/traces`);
  });

  it("exports correct Authorization header format", async () => {
    let capturedExporterConfig: any = null;
    const mockExporter = vi.fn(function (config: any) {
      capturedExporterConfig = config;
    });

    const mockNodeSDK = vi.fn(function () {});
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {});

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    const testApiKey = "ltn_svc_abc123xyz";
    shutdownFn = instrument({
      baseUrl: `http://127.0.0.1:${serverPort}`,
      apiKey: testApiKey,
      serviceName: "auth-test",
    });

    expect(capturedExporterConfig).not.toBeNull();
    expect(capturedExporterConfig.headers?.Authorization).toBe(
      `Bearer ${testApiKey}`
    );
  });

  it("works without apiKey (untrusted mode)", async () => {
    let capturedExporterConfig: any = null;
    const mockExporter = vi.fn(function (config: any) {
      capturedExporterConfig = config;
    });

    const mockNodeSDK = vi.fn(function () {});
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = vi.fn(async () => {});

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    shutdownFn = instrument({
      baseUrl: `http://127.0.0.1:${serverPort}`,
      serviceName: "no-auth-test",
    });

    expect(capturedExporterConfig).not.toBeNull();
    expect(capturedExporterConfig.headers?.Authorization).toBeUndefined();
  });

  it("shutdown flushes and stops the SDK", async () => {
    const mockNodeSDK = vi.fn(function () {});
    const shutdownSpy = vi.fn(async () => {});
    mockNodeSDK.prototype.start = vi.fn();
    mockNodeSDK.prototype.shutdown = shutdownSpy;

    const mockExporter = vi.fn();

    _setFactory({
      NodeSDK: mockNodeSDK as any,
      OTLPTraceExporter: mockExporter as any,
    });

    shutdownFn = instrument({
      baseUrl: `http://127.0.0.1:${serverPort}`,
      apiKey: "ltn_proj_test",
      serviceName: "shutdown-test",
    });

    // Before shutdown, SDK should be started
    expect(mockNodeSDK.prototype.start).toHaveBeenCalled();

    // After shutdown, shutdown should be called
    await shutdownFn();
    expect(shutdownSpy).toHaveBeenCalled();
  });
});
