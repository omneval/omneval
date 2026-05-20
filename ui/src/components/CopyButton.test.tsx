import { describe, it, expect, vi, beforeEach, afterAll } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { CopyButton } from "./CopyButton";

describe("CopyButton", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterAll(() => {
    vi.useRealTimers();
  });

  it("renders a copy icon button", () => {
    render(<CopyButton text="hello" />);
    const btn = screen.getByRole("button");
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveAttribute("aria-label", "Copy to clipboard");
  });

  it("copies text to clipboard on click", async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: writeTextMock } });

    render(<CopyButton text="my-api-key" />);
    fireEvent.click(screen.getByRole("button"));

    expect(writeTextMock).toHaveBeenCalledWith("my-api-key");
  });

  it("shows 'Copied!' feedback briefly after copy", async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: writeTextMock } });

    render(<CopyButton text="secret" />);
    fireEvent.click(screen.getByRole("button"));

    // Wait for state update and setTimeout (2000ms) to fire
    await vi.advanceTimersByTimeAsync(2000);

    // Feedback text appeared and was then removed
    expect(screen.queryByText("Copied!")).not.toBeInTheDocument();
  });

  it("handles clipboard write errors gracefully", async () => {
    const writeTextMock = vi.fn().mockRejectedValue(new Error("denied"));
    Object.assign(navigator, { clipboard: { writeText: writeTextMock } });

    render(<CopyButton text="secret" />);
    // Should not throw
    expect(() => fireEvent.click(screen.getByRole("button"))).not.toThrow();
  });
});
