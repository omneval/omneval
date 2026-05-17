import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { OnboardingEmptyState } from "./OnboardingEmptyState";

describe("OnboardingEmptyState", () => {
  it("renders the title and description", () => {
    render(<OnboardingEmptyState />);
    expect(screen.getByText("No traces yet")).toBeInTheDocument();
    expect(screen.getByText("Get started in 3 simple steps:")).toBeInTheDocument();
  });

  it("renders three step items", () => {
    render(<OnboardingEmptyState />);
    expect(screen.getByText("Install the SDK")).toBeInTheDocument();
    expect(screen.getByText("Create an API key")).toBeInTheDocument();
    expect(screen.getByText("Send your first trace")).toBeInTheDocument();
  });

  it("copy button copies the install command", async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: writeTextMock } });

    render(<OnboardingEmptyState />);
    fireEvent.click(screen.getByRole("button", { name: /install/i }));

    expect(writeTextMock).toHaveBeenCalledWith(
      "pip install lantern-sdk"
    );
  });

  it("copy button copies the API key command", async () => {
    const writeTextMock = vi.fn().mockResolvedValue(undefined);
    Object.assign(navigator, { clipboard: { writeText: writeTextMock } });

    render(<OnboardingEmptyState />);
    fireEvent.click(screen.getAllByRole("button", { name: /api key/i })[0]);

    expect(writeTextMock).toHaveBeenCalledWith(
      'curl -X POST http://localhost:8080/api/v1/projects/<project-id>/api-keys -H "Content-Type: application/json" -d \'{"kind":"project"}\''
    );
  });

  it("renders a link to the ingest docs", () => {
    render(<OnboardingEmptyState />);
    const link = screen.getByRole("link", { name: /ingest docs/i });
    expect(link).toHaveAttribute("href", "/docs/ingest");
  });
});
