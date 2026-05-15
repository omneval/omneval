import { useState, ReactNode } from "react";
import Sidebar from "./Sidebar";

interface LayoutProps {
  children: ReactNode;
  activeNav: string;
  onNavigate: (id: string) => void;
  onLogout?: () => void;
}

export default function Layout({ children, activeNav, onNavigate, onLogout }: LayoutProps) {
  const [collapsed, setCollapsed] = useState(false);

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
