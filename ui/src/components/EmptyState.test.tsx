import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { EmptyState, LoadingState } from "./EmptyState";

describe("EmptyState", () => {
  it("renders a default empty state with title and description", () => {
    render(<EmptyState />);
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    expect(screen.getByText("Create something to get started")).toBeInTheDocument();
  });

  it("renders with custom title and description", () => {
    render(
      <EmptyState
        title="Custom Title"
        description="Custom description"
      />
    );
    expect(screen.getByText("Custom Title")).toBeInTheDocument();
    expect(screen.getByText("Custom description")).toBeInTheDocument();
  });

  it("renders an action button when onAction is provided", () => {
    const handleAction = vi.fn();
    render(
      <EmptyState
        title="Get Started"
        actionLabel="Go"
        onAction={handleAction}
      />
    );
    expect(screen.getByText("Go")).toBeInTheDocument();
  });

  it("calls onAction when the action button is clicked", () => {
    const handleAction = vi.fn();
    render(
      <EmptyState
        title="Get Started"
        actionLabel="Go"
        onAction={handleAction}
      />
    );
    screen.getByText("Go").click();
    expect(handleAction).toHaveBeenCalledTimes(1);
  });

  it("renders the onboarding variant with the lantern icon", () => {
    render(<EmptyState variant="onboarding" />);
    expect(screen.getByText("No traces yet")).toBeInTheDocument();
  });

  it("renders the search variant", () => {
    render(<EmptyState variant="search" />);
    expect(screen.getByText("No results found")).toBeInTheDocument();
  });

  it("renders the error variant with danger color", () => {
    render(<EmptyState variant="error" />);
    expect(screen.getByText("Something went wrong")).toBeInTheDocument();
  });

  it("renders a custom icon when provided", () => {
    const { container } = render(
      <EmptyState icon={<span data-testid="custom-icon">★</span>} />
    );
    expect(container.querySelector('[data-testid="custom-icon"]')).toBeInTheDocument();
  });

  it("renders role=status for accessibility", () => {
    const { container } = render(<EmptyState />);
    expect(container.firstElementChild).toHaveAttribute("role", "status");
  });

  it("applies custom className", () => {
    const { container } = render(<EmptyState className="custom-class" />);
    expect(container.firstElementChild).toHaveClass("custom-class");
  });

  it("applies custom style", () => {
    const { container } = render(
      <EmptyState style={{ padding: "2rem" }} />
    );
    expect(container.firstElementChild).toHaveStyle({ padding: "2rem" });
  });
});

describe("LoadingState", () => {
  it("renders skeleton rows by default", () => {
    const { container } = render(<LoadingState rows={3} />);
    const skeletons = container.querySelectorAll("[class*='animate-pulse']");
    expect(skeletons).toHaveLength(3);
  });

  it("renders 6 rows by default", () => {
    const { container } = render(<LoadingState />);
    const skeletons = container.querySelectorAll("[class*='animate-pulse']");
    expect(skeletons).toHaveLength(6);
  });

  it("shows title skeleton when showTitle is true", () => {
    const { container } = render(<LoadingState showTitle rows={2} />);
    const skeletons = container.querySelectorAll("[class*='animate-pulse']");
    expect(skeletons).toHaveLength(3); // 1 title + 2 rows
  });

  it("applies custom className", () => {
    const { container } = render(<LoadingState className="custom-class" />);
    expect(container.firstElementChild).toHaveClass("custom-class");
  });
});
