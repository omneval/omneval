import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import Sidebar from "./Sidebar";

describe("Sidebar", () => {
  const defaultProps = {
    collapsed: false,
    onToggle: vi.fn(),
    active: "traces",
    onNavigate: vi.fn(),
    onLogout: vi.fn(),
  };

  it("renders a Traces navigation item in the home section", () => {
    render(<Sidebar {...defaultProps} />);
    expect(screen.getByText("Traces")).toBeInTheDocument();
  });

  it("navigates to the Traces page when clicking the Traces item", () => {
    const onNavigate = vi.fn();
    render(<Sidebar {...defaultProps} onNavigate={onNavigate} />);
    fireEvent.click(screen.getByText("Traces"));
    expect(onNavigate).toHaveBeenCalledWith("traces");
  });

  it("highlights the Traces item as active when active='traces'", () => {
    render(<Sidebar {...defaultProps} active="traces" />);
    const tracesButton = screen.getByRole("button", { name: /Traces/i });
    expect(tracesButton).toHaveClass("text-lantern-ember");
  });

  it("does not highlight the Traces item when active='dashboard'", () => {
    render(<Sidebar {...defaultProps} active="dashboard" />);
    const tracesButton = screen.getByRole("button", { name: /Traces/i });
    expect(tracesButton).not.toHaveClass("text-lantern-ember");
  });

  it("has an appropriate icon for the Traces nav item", () => {
    render(<Sidebar {...defaultProps} />);
    const tracesButton = screen.getByRole("button", { name: /Traces/i });
    const svg = tracesButton.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });

  it("renders the Traces item even when another section is active", () => {
    render(<Sidebar {...defaultProps} active="settings" />);
    expect(screen.getByText("Traces")).toBeInTheDocument();
  });
});
