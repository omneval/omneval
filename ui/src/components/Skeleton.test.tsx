import { describe, it, expect } from "vitest";
import { render } from "@testing-library/react";
import { Skeleton } from "./Skeleton";

describe("Skeleton", () => {
  it("renders a shimmer skeleton block with base classes", () => {
    const { container } = render(<Skeleton />);
    const el = container.firstElementChild;
    expect(el).not.toBeNull();
    expect(el?.className).toContain("rounded");
    expect(el?.className).toContain("animate-pulse");
    expect(el?.className).toContain("bg-lantern-bg-cave");
  });

  it("accepts a width style prop", () => {
    const { container } = render(<Skeleton style={{ width: "120px" }} />);
    const el = container.firstElementChild as HTMLElement | null;
    expect(el?.style.width).toBe("120px");
  });

  it("accepts a height style prop", () => {
    const { container } = render(<Skeleton style={{ height: "16px" }} />);
    const el = container.firstElementChild as HTMLElement | null;
    expect(el?.style.height).toBe("16px");
  });

  it("renders as a div by default", () => {
    const { container } = render(<Skeleton />);
    expect(container.firstElementChild?.tagName).toBe("DIV");
  });

  it("passes through additional className props", () => {
    const { container } = render(<Skeleton className="my-custom-skeleton" />);
    const el = container.firstElementChild;
    expect(el?.className).toContain("my-custom-skeleton");
  });
});
