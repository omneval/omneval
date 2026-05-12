import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import Breadcrumb from "./Breadcrumb";

describe("Breadcrumb", () => {
  it("renders breadcrumb items as links", () => {
    render(
      <Breadcrumb items={[
        { label: "Home" },
        { label: "Traces" },
        { label: "abc123" },
      ]} />
    );

    expect(screen.getByText("Home")).toBeInTheDocument();
    expect(screen.getByText("Traces")).toBeInTheDocument();
    expect(screen.getByText("abc123")).toBeInTheDocument();
  });

  it("renders separator arrows between items", () => {
    const { container } = render(
      <Breadcrumb items={[
        { label: "Home" },
        { label: "Traces" },
      ]} />
    );

    // There should be 1 arrow between 2 items
    const arrows = container.querySelectorAll("svg");
    expect(arrows.length).toBe(1);
  });

  it("calls onClick on non-last items", () => {
    const handleClick = vi.fn();
    render(
      <Breadcrumb items={[
        { label: "Home", onClick: handleClick },
        { label: "Detail" },
      ]} />
    );

    fireEvent.click(screen.getByText("Home"));
    expect(handleClick).toHaveBeenCalledTimes(1);
  });

  it("does not call onClick on last item", () => {
    const handleClick = vi.fn();
    render(
      <Breadcrumb items={[
        { label: "Home", onClick: handleClick },
        { label: "Detail", onClick: handleClick },
      ]} />
    );

    fireEvent.click(screen.getByText("Detail"));
    expect(handleClick).not.toHaveBeenCalled();
  });

  it("renders nothing when items is empty", () => {
    const { container } = render(<Breadcrumb items={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("marks last item with aria-current=page", () => {
    render(
      <Breadcrumb items={[
        { label: "Home" },
        { label: "Detail" },
      ]} />
    );

    const detail = screen.getByText("Detail");
    expect(detail).toHaveAttribute("aria-current", "page");
  });
});
