import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { ErrorBanner } from "./ErrorBanner";

describe("ErrorBanner with retry", () => {
  it("shows a retry button when onRetry is provided", () => {
    const onRetry = vi.fn();
    render(
      <ErrorBanner message="API connection failed" onRetry={onRetry} retryLabel="Retry" />
    );
    expect(screen.getByRole("button", { name: "Retry" })).toBeInTheDocument();
  });

  it("calls onRetry when retry button is clicked", () => {
    const onRetry = vi.fn();
    render(
      <ErrorBanner message="API connection failed" onRetry={onRetry} retryLabel="Retry" />
    );
    fireEvent.click(screen.getByRole("button", { name: "Retry" }));
    expect(onRetry).toHaveBeenCalledTimes(1);
  });

  it("shows dismiss button and retry button together when both handlers provided", () => {
    const onDismiss = vi.fn();
    const onRetry = vi.fn();
    render(
      <ErrorBanner
        message="Something went wrong"
        onDismiss={onDismiss}
        onRetry={onRetry}
        retryLabel="Try again"
      />
    );
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Dismiss" })).toBeInTheDocument();
  });

  it("hides retry button when onRetry is not provided", () => {
    render(<ErrorBanner message="Error" />);
    expect(screen.queryByRole("button", { name: /retry|try again/i })).not.toBeInTheDocument();
  });
});
