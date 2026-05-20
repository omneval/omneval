import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ErrorBoundary } from "./ErrorBanner";

function FailingChild(): React.ReactElement {
  throw new Error("Render failure");
}

function WorkingChild(): React.ReactElement {
  return <div>OK</div>;
}

describe("ErrorBoundary", () => {
  beforeEach(() => {
    vi.spyOn(console, "error").mockImplementation(() => {});
  });

  it("renders children normally when no error occurs", () => {
    render(
      <ErrorBoundary>
        <WorkingChild />
      </ErrorBoundary>
    );
    expect(screen.getByText("OK")).toBeInTheDocument();
  });

  it("renders fallback UI when child throws", () => {
    render(
      <ErrorBoundary>
        <FailingChild />
      </ErrorBoundary>
    );
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /reload/i })).toBeInTheDocument();
  });

  it("reloads the page when Reload button is clicked", () => {
    const reloadMock = vi.fn();
    Object.defineProperty(window, "location", {
      value: { reload: reloadMock },
      writable: true,
    });

    render(
      <ErrorBoundary>
        <FailingChild />
      </ErrorBoundary>
    );

    fireEvent.click(screen.getByRole("button", { name: /reload/i }));
    expect(reloadMock).toHaveBeenCalled();
  });

  it("renders custom fallback when provided", () => {
    render(
      <ErrorBoundary
        fallback={
          <div data-testid="custom-fallback">Custom Error</div>
        }
      >
        <FailingChild />
      </ErrorBoundary>
    );
    expect(screen.getByTestId("custom-fallback")).toBeInTheDocument();
    // Should NOT render the default fallback
    expect(screen.queryByText("Something went wrong")).not.toBeInTheDocument();
  });
});
