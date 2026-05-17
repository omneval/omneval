import { useState, useEffect, ReactNode } from "react";
import Sidebar from "./Sidebar";

interface LayoutProps {
  children: ReactNode;
  activeNav: string;
  onNavigate: (id: string) => void;
  onLogout?: () => void;
}

export default function Layout({ children, activeNav, onNavigate, onLogout }: LayoutProps) {
  const [collapsed, setCollapsed] = useState(false);

  // Auto-collapse sidebar on narrow viewports
  useEffect(() => {
    const handleResize = () => {
      if (window.innerWidth < 1024 && !collapsed) {
        setCollapsed(true);
      }
    };
    if (window.innerWidth < 1024) {
      setCollapsed(true);
    }
    window.addEventListener("resize", handleResize);
    return () => window.removeEventListener("resize", handleResize);
  }, [collapsed]);

  return (
    <div className="flex h-screen bg-lantern-bg-abyss">
      <Sidebar
        collapsed={collapsed}
        onToggle={() => setCollapsed(!collapsed)}
        active={activeNav}
        onNavigate={onNavigate}
        onLogout={onLogout}
      />
      <main className="flex-1 overflow-y-auto">{children}</main>
    </div>
  );
}
