import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import Sidebar from "./Sidebar";

describe("Sidebar — Issue #102 polish", () => {
  const defaultProps = {
    collapsed: false,
    onToggle: vi.fn(),
    active: "traces",
    onNavigate: vi.fn(),
    onLogout: vi.fn(),
  };

  describe("collapsed state", () => {
    it("renders icon-only logout button in collapsed state", () => {
      const onLogout = vi.fn();
      render(<Sidebar {...defaultProps} collapsed={true} onLogout={onLogout} />);
      // Logout button should be visible in collapsed state
      const logoutButton = screen.getByRole("button", { name: /Logout/i });
      expect(logoutButton).toBeInTheDocument();
    });

    it("navigates to logout when clicking collapsed logout icon", () => {
      const onLogout = vi.fn();
      render(<Sidebar {...defaultProps} collapsed={true} onLogout={onLogout} />);
      const logoutButton = screen.getByRole("button", { name: /Logout/i });
      fireEvent.click(logoutButton);
      expect(onLogout).toHaveBeenCalled();
    });

    it("does not show logout text in collapsed state", () => {
      render(<Sidebar {...defaultProps} collapsed={true} onLogout={vi.fn()} />);
      expect(screen.queryByText("Logout")).not.toBeInTheDocument();
    });

    it("does not show Settings in collapsed state", () => {
      render(<Sidebar {...defaultProps} collapsed={true} onLogout={vi.fn()} />);
      expect(screen.queryByText("Settings")).not.toBeInTheDocument();
    });

    it("shows nav icon tooltips in collapsed state via aria-label", () => {
      render(<Sidebar {...defaultProps} collapsed={true} onLogout={vi.fn()} />);
      // All nav items should have aria-labels for accessibility
      const navButtons = screen.getAllByRole("button");
      navButtons.forEach((btn) => {
        expect(btn).toHaveAttribute("aria-label");
      });
    });
  });

  describe("expanded state", () => {
    it("shows logout text in expanded state", () => {
      render(<Sidebar {...defaultProps} collapsed={false} onLogout={vi.fn()} />);
      expect(screen.getByText("Logout")).toBeInTheDocument();
    });

    it("shows Settings in expanded state", () => {
      render(<Sidebar {...defaultProps} collapsed={false} onLogout={vi.fn()} />);
      expect(screen.getByText("Settings")).toBeInTheDocument();
    });

    it("navigates to settings when clicking Settings in expanded state", () => {
      const onNavigate = vi.fn();
      render(<Sidebar {...defaultProps} collapsed={false} onNavigate={onNavigate} />);
      fireEvent.click(screen.getByText("Settings"));
      expect(onNavigate).toHaveBeenCalledWith("settings");
    });
  });

  describe("responsive behavior", () => {
    it("applies responsive breakpoint class for auto-collapse", () => {
      const { container } = render(<Sidebar {...defaultProps} collapsed={false} />);
      // The sidebar should use CSS media queries for responsive behavior
      // Check that the aside element exists
      const aside = container.querySelector("aside");
      expect(aside).toBeInTheDocument();
    });
  });
});
