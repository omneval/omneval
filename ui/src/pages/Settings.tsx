import { useState, useEffect, useCallback } from "react";
import { useToast } from "@/components/Toast";
import { CopyButton } from "@/components/CopyButton";
import { Skeleton } from "@/components/Skeleton";
import { colors } from "@/theme";

// ── Types ──────────────────────────────────────────────────────────

interface APIKey {
  key_id: string;
  kind: "project" | "service";
  service_name?: string;
  created_at: string;
  revoked_at?: string;
}

interface Project {
  project_id: string;
  name: string;
  org_id: string;
}

// ── Components ─────────────────────────────────────────────────────

function SectionHeader({ title, description }: { title: string; description: string }) {
  return (
    <div className="mb-6">
      <h2 className="text-lg font-medium text-lantern-pure">{title}</h2>
      <p className="text-sm text-lantern-ash mt-1">{description}</p>
    </div>
  );
}

function KeyCard({
  apiKey,
  onRevoke,
}: {
  apiKey: APIKey;
  onRevoke: (keyId: string) => void;
}) {
  const isRevoked = apiKey.revoked_at !== undefined;

  return (
    <div
      className={`flex items-center justify-between px-4 py-3 rounded-md border ${
        isRevoked
          ? "border-lantern-bg-cave bg-lantern-bg-charcoal/50 opacity-60"
          : "border-lantern-bg-cave bg-lantern-bg-charcoal"
      }`}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-mono text-lantern-pure truncate">
            {apiKey.key_id}
          </span>
          {!isRevoked && (
            <CopyButton
              text={apiKey.key_id}
              copyLabel="Copy"
              ariaLabel={`Copy API key ${apiKey.kind}`}
            />
          )}
          <span
            className={`text-xs px-1.5 py-0.5 rounded font-medium ${
              apiKey.kind === "service"
                ? "text-lantern-ember bg-lantern-accent-ember-glow"
                : "text-lantern-ash bg-lantern-bg-illumination"
            }`}
          >
            {apiKey.kind === "service" ? "Service" : "Project"}
          </span>
          {isRevoked && (
            <span className="text-xs text-lantern-ember bg-lantern-accent-ember-glow px-1.5 py-0.5 rounded font-medium">
              Revoked
            </span>
          )}
        </div>
        {apiKey.service_name && (
          <p className="text-xs text-lantern-ash mt-1">
            Service: <span className="text-lantern-pure">{apiKey.service_name}</span>
          </p>
        )}
        <p className="text-xs text-lantern-ash mt-0.5">
          Created: {new Date(apiKey.created_at).toLocaleDateString()}
        </p>
      </div>
      {!isRevoked && (
        <button
          onClick={() => onRevoke(apiKey.key_id)}
          className="ml-3 btn-destructive text-xs py-1"
        >
          Revoke
        </button>
      )}
    </div>
  );
}

function GenerateKeyDialog({
  projectId,
  kind,
  onClose,
  onGenerated,
}: {
  projectId: string;
  kind: "project" | "service";
  onClose: () => void;
  onGenerated: (rawKey: string) => void;
}) {
  const { addToast } = useToast();
  const [serviceName, setServiceName] = useState("");
  const [loading, setLoading] = useState(false);
  const [rawKey, setRawKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (kind === "service" && serviceName.trim() === "") return;

    setLoading(true);
    try {
      const body: Record<string, string> = { kind };
      if (kind === "service") body.service_name = serviceName.trim();

      const res = await fetch(`/api/v1/projects/${projectId}/api-keys`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        if (err.error) {
          addToast("error", err.error);
        } else {
          addToast("error", "Failed to generate API key");
        }
        return;
      }

      const data = await res.json();
      setRawKey(data.raw_key);
    } catch {
      addToast("error", "Failed to generate API key");
    } finally {
      setLoading(false);
    }
  };

  const handleCopy = () => {
    if (!rawKey) return;
    navigator.clipboard.writeText(rawKey);
    setCopied(true);
    setTimeout(() => setCopied(false), 2000);
  };

  const handleClose = () => {
    setRawKey(null);
    setServiceName("");
    setCopied(false);
    onClose();
  };

  if (rawKey) {
    return (
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
        <div
          className="bg-lantern-bg-charcoal border border-lantern-bg-cave rounded-lg p-6 w-full max-w-md mx-4"
          style={{ boxShadow: "0 16px 48px rgba(0,0,0,0.5)" }}
        >
          <h3 className="text-lg font-medium text-lantern-pure mb-2">
            Your API Key
          </h3>
          <p className="text-sm text-lantern-ash mb-4">
            This is the only time your key will be shown. Store it securely.
          </p>
          <div className="bg-lantern-bg-charcoal border border-lantern-bg-cave rounded-md p-3 mb-4">
            <code className="text-sm font-mono text-lantern-pure break-all">
              {rawKey}
            </code>
          </div>
          <div className="flex gap-3">
            <button
              onClick={() => {
                handleCopy();
                onGenerated(rawKey);
                addToast("success", `${kind === "service" ? "Service" : "Project"} API key generated`);
                handleClose();
              }}
              className={`flex-1 px-3 py-2 rounded-md text-sm font-medium text-white transition-all duration-150 hover:brightness-110 ${
                copied
                  ? ""
                  : "bg-lantern-bg-illumination border border-lantern-bg-cave text-lantern-pure hover:border-lantern-accent-ember hover:text-lantern-ember"
              }`}
              style={copied ? {
                background: colors.accents.emberFlare,
                boxShadow: "0 2px 8px rgba(255, 87, 34, 0.25)",
              } : undefined}
            >
              {copied ? "Copied!" : "Copy Key"}
            </button>
            <button
              onClick={handleClose}
              className="btn-secondary text-sm py-1.5"
            >
              Close
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div
        className="bg-lantern-bg-charcoal border border-lantern-bg-cave rounded-lg p-6 w-full max-w-md mx-4"
        style={{ boxShadow: "0 16px 48px rgba(0,0,0,0.5)" }}
      >
        <h3 className="text-lg font-medium text-lantern-pure mb-4">
          Generate {kind === "service" ? "Service" : "Project"} API Key
        </h3>
        <form onSubmit={handleSubmit}>
          {kind === "service" && (
            <div className="mb-4">
              <label className="block text-sm font-medium text-lantern-pure mb-1">
                Service Name
              </label>
              <input
                type="text"
                value={serviceName}
                onChange={(e) => setServiceName(e.target.value)}
                placeholder="e.g. my-agent"
                className="input-focus w-full px-3 py-2 rounded-md border border-lantern-bg-cave bg-lantern-bg-abyss text-lantern-pure placeholder-lantern-ash/50 text-sm"
                required
                autoFocus
              />
            </div>
          )}
          <div className="flex gap-3">
            <button
              type="submit"
              disabled={loading || (kind === "service" && serviceName.trim() === "")}
              className="flex-1 btn-primary text-sm py-1.5 disabled:opacity-50"
            >
              {loading ? "Generating..." : "Generate Key"}
            </button>
            <button
              type="button"
              onClick={handleClose}
              className="btn-secondary text-sm py-1.5"
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

export function NewProjectModal({
  onClose,
  onCreated,
}: {
  onClose: () => void;
  onCreated: (project: Project) => void;
}) {
  const { addToast } = useToast();
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (name.trim() === "") return;

    setLoading(true);
    try {
      const res = await fetch("/api/v1/projects", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name: name.trim() }),
      });

      if (!res.ok) {
        const err = await res.json().catch(() => ({}));
        if (err.error) {
          addToast("error", err.error);
        } else {
          addToast("error", "Failed to create project");
        }
        return;
      }

      const data = await res.json();
      addToast("success", `Project "${data.name || name.trim()}" created`);
      onCreated(data);
      onClose();
    } catch {
      addToast("error", "Failed to create project");
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div
        className="bg-lantern-bg-charcoal border border-lantern-bg-cave rounded-lg p-6 w-full max-w-md mx-4"
        style={{ boxShadow: "0 16px 48px rgba(0,0,0,0.5)" }}
      >
        <h3 className="text-lg font-medium text-lantern-pure mb-4">
          New Project
        </h3>
        <form onSubmit={handleSubmit}>
          <div className="mb-4">
            <label className="block text-sm font-medium text-lantern-pure mb-1">
              Project Name
            </label>
            <input
              type="text"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. my-agent"
              className="input-focus w-full px-3 py-2 rounded-md border border-lantern-bg-cave bg-lantern-bg-abyss text-lantern-pure placeholder-lantern-ash/50 text-sm"
              required
              autoFocus
            />
          </div>
          <div className="flex gap-3">
            <button
              type="submit"
              disabled={loading || name.trim() === ""}
              className="flex-1 btn-primary text-sm py-1.5 disabled:opacity-50"
            >
              {loading ? "Creating..." : "Create Project"}
            </button>
            <button
              type="button"
              onClick={onClose}
              className="btn-secondary text-sm py-1.5"
            >
              Cancel
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function ConfirmDialog({
  message,
  onConfirm,
  onCancel,
}: {
  message: string;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60">
      <div
        className="bg-lantern-bg-charcoal border border-lantern-bg-cave rounded-lg p-6 w-full max-w-sm mx-4"
        style={{ boxShadow: "0 16px 48px rgba(0,0,0,0.5)" }}
      >
        <p className="text-sm text-lantern-pure mb-4">{message}</p>
        <div className="flex gap-3">
          <button
            onClick={onConfirm}
            className="flex-1 px-3 py-2 rounded-md text-sm font-medium bg-lantern-accent-ember-glow text-lantern-ember hover:bg-lantern-accent-ember transition-colors"
          >
            Confirm
          </button>
          <button
            onClick={onCancel}
            className="flex-1 px-3 py-2 rounded-md text-sm bg-lantern-bg-illumination border border-lantern-bg-cave text-lantern-ash hover:text-lantern-pure transition-colors"
          >
            Cancel
          </button>
        </div>
      </div>
    </div>
  );
}

// ── Main Settings Page ─────────────────────────────────────────────

export default function SettingsPage({
  activeProject,
}: {
  activeProject: string;
}) {
  const { addToast } = useToast();
  const [apiKeys, setApiKeys] = useState<APIKey[]>([]);
  const [loading, setLoading] = useState(true);
  const [generateKind, setGenerateKind] = useState<"project" | "service" | null>(null);
  const [revokeKeyId, setRevokeKeyId] = useState<string | null>(null);

  const fetchKeys = useCallback(async () => {
    try {
      const res = await fetch(`/api/v1/projects/${activeProject}/api-keys`);
      if (res.ok) {
        const data = await res.json();
        setApiKeys(data);
      }
    } catch {
      // Silently ignore
    } finally {
      setLoading(false);
    }
  }, [activeProject]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const handleRevoke = (keyId: string) => {
    setRevokeKeyId(keyId);
  };

  const confirmRevoke = async () => {
    if (!revokeKeyId) return;
    try {
      const res = await fetch(
        `/api/v1/projects/${activeProject}/api-keys/${revokeKeyId}`,
        { method: "DELETE" }
      );
      if (res.ok) {
        addToast("success", "API key revoked");
        await fetchKeys();
      } else {
        addToast("error", "Failed to revoke key");
      }
    } catch {
      addToast("error", "Failed to revoke key");
    }
    setRevokeKeyId(null);
  };

  return (
    <div className="flex flex-col gap-8 py-6 px-6">
      {/* API Keys Section */}
      <div>
        <SectionHeader
          title="API Keys"
          description="Manage API keys for your active project. Keys are shown only once when generated."
        />

        {/* Generate buttons */}
        <div className="flex gap-2 mb-4">
          <button
            onClick={() => setGenerateKind("project")}
            className="btn-primary text-sm py-1.5"
          >
            + New Project Key
          </button>
          <button
            onClick={() => setGenerateKind("service")}
            className="btn-primary text-sm py-1.5"
          >
            + New Service Key
          </button>
        </div>

        {/* Key list */}
        {loading ? (
          <div className="space-y-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-16 rounded-md" />
            ))}
          </div>
        ) : apiKeys.length === 0 ? (
          <div className="text-sm text-lantern-ash py-8 text-center bg-lantern-bg-charcoal/30 rounded-md border border-lantern-bg-cave">
            No API keys yet. Generate one to start ingesting traces.
          </div>
        ) : (
          <div className="space-y-2">
            {apiKeys.map((k) => (
              <KeyCard key={k.key_id} apiKey={k} onRevoke={handleRevoke} />
            ))}
          </div>
        )}
      </div>

      {/* Modals */}
      {generateKind && (
        <GenerateKeyDialog
          projectId={activeProject}
          kind={generateKind}
          onClose={() => setGenerateKind(null)}
          onGenerated={() => {
            setGenerateKind(null);
            fetchKeys();
          }}
        />
      )}

      {revokeKeyId && (
        <ConfirmDialog
          message="Are you sure you want to revoke this API key? This action cannot be undone."
          onConfirm={confirmRevoke}
          onCancel={() => setRevokeKeyId(null)}
        />
      )}
    </div>
  );
}
