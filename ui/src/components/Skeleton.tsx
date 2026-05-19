import React from "react";

interface SkeletonProps extends React.HTMLAttributes<HTMLDivElement> {
  /** Override width. Defaults to full width. */
  width?: string;
  /** Override height. Defaults to 1rem. */
  height?: string;
}

/**
 * Skeleton — Animated shimmer placeholder for loading states.
 * Uses Tailwind's `animate-pulse` and the omneval border color
 * to match the dark theme aesthetic.
 */
export function Skeleton({
  width,
  height = "1rem",
  style,
  className = "",
  ...rest
}: SkeletonProps) {
  return (
    <div
      className={`animate-pulse rounded bg-omneval-border ${className}`}
      style={{
        width: width ?? "100%",
        height,
        ...style,
      }}
      {...rest}
    />
  );
}
