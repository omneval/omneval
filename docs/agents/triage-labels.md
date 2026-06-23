# Triage Labels

The skills speak in terms of five canonical triage roles. This file maps those roles to the actual label strings used in this repo's issue tracker (`omneval/omneval` on GitHub).

| Label in mattpocock/skills | Label in our tracker | Meaning                                  |
| -------------------------- | --------------------- | ----------------------------------------- |
| `needs-triage`             | `needs-triage`        | Maintainer needs to evaluate this issue   |
| `needs-info`               | `needs-info`          | Waiting on reporter for more information  |
| `ready-for-agent`          | `agent-ready`         | Fully specified, ready for an AFK agent — this repo's pre-existing label, kept as-is rather than creating a duplicate |
| `ready-for-human`          | `ready-for-human`     | Requires human implementation             |
| `wontfix`                  | `wontfix`             | Will not be actioned                      |

When a skill mentions a role (e.g. "apply the AFK-ready triage label"), use the corresponding label string from the right-hand column — i.e. apply `agent-ready`, not `ready-for-agent`, for that role.

Other labels exist in this repo (`bug`, `documentation`, `enhancement`, `prd`, `sandcastle`, `devloop-code-quality`, `performance`, `writer`, etc.) but aren't part of the canonical triage state machine — apply them as normal GitHub labels when relevant, separately from the triage role.
