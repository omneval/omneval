{{- /*
Omneval Helm Chart — Shared Helpers
*/ -}}

{{- /*
omneval.name returns a sanitized release name.
*/ -}}
{{- define "omneval.name" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- /*
omneval.labels returns standard Kubernetes labels shared across all resources.
*/ -}}
{{- define "omneval.labels" -}}
app.kubernetes.io/name: omneval
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- /*
omneval.selectorLabels returns labels used in selector matchLabels.
*/ -}}
{{- define "omneval.selectorLabels" -}}
app.kubernetes.io/name: omneval
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- /*
omneval.componentLabels returns labels for a specific component.

Usage: {{ include "omneval.componentLabels" (merge (dict "component" "writer") .) }}
*/ -}}
{{- define "omneval.componentLabels" -}}
app.kubernetes.io/component: {{ .component }}
{{- end -}}

{{- /*
omneval.fullLabels renders full labels with proper indentation.

Usage: {{ include "omneval.fullLabels" (merge (dict "component" "writer" "indent" 4) .) | nindent .indent }}
*/ -}}
{{- define "omneval.fullLabels" -}}
{{- $labels := include "omneval.labels" . -}}
{{- $compLabels := include "omneval.componentLabels" (merge (dict "component" .component) .) -}}
{{ $labels | trim }}
{{ $compLabels | trim }}
{{- end -}}

{{- /*
omneval.redisPassword determines whether a Redis password reference should be included.

Returns "redis-password" if Redis password should be configured (via internal auth
or external connection), empty string otherwise.

Usage: {{ include "omneval.redisPassword" . }}
*/ -}}
{{- define "omneval.redisPassword" -}}
{{- if and .Values.redis.enabled .Values.redis.auth.enabled .Values.redis.auth.password -}}
redis-password
{{- else if .Values.redis.external.password -}}
redis-password
{{- end }}
{{- end -}}

{{- /*
omneval.storageEndpoint returns the S3-compatible storage endpoint URL.

Returns empty string if no storage is configured.

Usage: {{ include "omneval.storageEndpoint" . }}
*/ -}}
{{- define "omneval.storageEndpoint" -}}
{{- if .Values.minio.enabled -}}
{{- printf "http://%s-minio:9000" .Release.Name -}}
{{- else if .Values.storage.endpoint -}}
{{- .Values.storage.endpoint -}}
{{- end }}
{{- end -}}

{{- /*
omneval.storageAccessKey returns the S3 access key.

Usage: {{ include "omneval.storageAccessKey" . }}
*/ -}}
{{- define "omneval.storageAccessKey" -}}
{{- if .Values.storage.accessKey -}}
{{- .Values.storage.accessKey -}}
{{- else if .Values.minio.enabled -}}
{{- .Values.minio.rootUser -}}
{{- end }}
{{- end -}}

{{- /*
omneval.storageSecretKey returns the S3 secret key.

Usage: {{ include "omneval.storageSecretKey" . }}
*/ -}}
{{- define "omneval.storageSecretKey" -}}
{{- if .Values.storage.secretKey -}}
{{- .Values.storage.secretKey -}}
{{- else if .Values.minio.enabled -}}
{{- .Values.minio.rootPassword -}}
{{- end }}
{{- end -}}

{{- /*
omneval.storageBucket returns the S3 bucket name.

When MinIO is enabled internally, the bundled bucket-creation Job provisions a
bucket named "omneval", so that is used as the default unless storage.bucket is
explicitly set.

Usage: {{ include "omneval.storageBucket" . }}
*/ -}}
{{- define "omneval.storageBucket" -}}
{{- if .Values.storage.bucket -}}
{{- .Values.storage.bucket -}}
{{- else if .Values.minio.enabled -}}
omneval
{{- end }}
{{- end -}}

{{- /*
omneval.storageSecret returns the name of the Secret that holds the S3
access key and secret key. Returns the user-provided storage.existingSecret
when set, otherwise the chart-managed release Secret.

Usage: {{ include "omneval.storageSecret" . }}
*/ -}}
{{- define "omneval.storageSecret" -}}
{{- if .Values.storage.existingSecret -}}
{{- .Values.storage.existingSecret -}}
{{- else -}}
{{- printf "%s-secret" .Release.Name -}}
{{- end }}
{{- end -}}

{{- /*
omneval.storageAccessKeySecretKey returns the key inside the storage Secret
that holds the S3 access key. Returns storage.existingSecretAccessKeyKey when
storage.existingSecret is set, otherwise "storage-access-key" (the key used
in the chart-managed omneval-secret).

Usage: {{ include "omneval.storageAccessKeySecretKey" . }}
*/ -}}
{{- define "omneval.storageAccessKeySecretKey" -}}
{{- if .Values.storage.existingSecret -}}
{{- .Values.storage.existingSecretAccessKeyKey -}}
{{- else -}}
storage-access-key
{{- end }}
{{- end -}}

{{- /*
omneval.storageSecretKeySecretKey returns the key inside the storage Secret
that holds the S3 secret key. Returns storage.existingSecretSecretKeyKey when
storage.existingSecret is set, otherwise "storage-secret-key" (the key used
in the chart-managed omneval-secret).

Usage: {{ include "omneval.storageSecretKeySecretKey" . }}
*/ -}}
{{- define "omneval.storageSecretKeySecretKey" -}}
{{- if .Values.storage.existingSecret -}}
{{- .Values.storage.existingSecretSecretKeyKey -}}
{{- else -}}
storage-secret-key
{{- end }}
{{- end -}}

{{- /*
omneval.storageHasExistingSecret returns true when storage.existingSecret is
set.

Usage: {{ include "omneval.storageHasExistingSecret" . }}
*/ -}}
{{- define "omneval.storageHasExistingSecret" -}}
{{- if .Values.storage.existingSecret -}}
true
{{- end }}
{{- end -}}

{{- /*
omneval.postgresDsn returns the PostgreSQL DSN string.

Usage: {{ include "omneval.postgresDsn" . }}
*/ -}}
{{- define "omneval.postgresDsn" -}}
{{- if .Values.postgresql.enabled -}}
{{- printf "postgres://%s:%s@%s-postgresql:5432/%s?sslmode=disable"
  .Values.postgresql.auth.username
  .Values.postgresql.auth.password
  .Release.Name
  .Values.postgresql.auth.database -}}
{{- else if .Values.postgresql.external.host -}}
{{- printf "postgres://%s:%s@%s:%d/%s?sslmode=%s"
  .Values.postgresql.external.user
  .Values.postgresql.external.password
  .Values.postgresql.external.host
  .Values.postgresql.external.port
  .Values.postgresql.external.database
  .Values.postgresql.external.sslmode -}}
{{- else if .Values.database.dsn -}}
{{- .Values.database.dsn -}}
{{- end }}
{{- end -}}

{{- /*
omneval.redisAddr returns the Redis connection address.

Usage: {{ include "omneval.redisAddr" . }}
*/ -}}
{{- define "omneval.redisAddr" -}}
{{- if .Values.redis.enabled -}}
{{- printf "%s-redis:6379" .Release.Name -}}
{{- else if .Values.redis.external.addr -}}
{{- .Values.redis.external.addr -}}
{{- end }}
{{- end -}}

{{- /*
omneval.quackServerAddr returns the in-cluster DNS address of the Quack
Server (host:port, no scheme), used by Writer/Query/Eval as
quack.client.url.

Usage: {{ include "omneval.quackServerAddr" . }}
*/ -}}
{{- define "omneval.quackServerAddr" -}}
{{- if .Values.quack.client.url -}}
{{- .Values.quack.client.url -}}
{{- else -}}
{{- $port := trimPrefix ":" .Values.quack.server.listenAddr -}}
{{- printf "%s-quack-server:%s" .Release.Name $port -}}
{{- end }}
{{- end -}}

{{- /*
omneval.quackDataPath returns the Lake data path shared by quack.server and
quack.client config. Derived from storage.bucket (s3://<bucket>/lake) when
S3 is configured, otherwise a local path on the quack-server PVC.

Usage: {{ include "omneval.quackDataPath" . }}
*/ -}}
{{- define "omneval.quackDataPath" -}}
{{- $bucket := include "omneval.storageBucket" . -}}
{{- if $bucket -}}
{{- printf "s3://%s/lake" $bucket -}}
{{- else -}}
/data/lake
{{- end }}
{{- end -}}

{{- /*
omneval.quackSecret returns the name of the Secret that holds the Quack auth
token. Returns the user-provided existingSecret when set, otherwise the
chart-managed release Secret.

Usage: {{ include "omneval.quackSecret" . }}
*/ -}}
{{- define "omneval.quackSecret" -}}
{{- if .Values.quack.server.existingSecret -}}
{{- .Values.quack.server.existingSecret -}}
{{- else -}}
{{- printf "%s-secret" .Release.Name -}}
{{- end }}
{{- end -}}

{{- /*
omneval.quackSecretKey returns the key inside the Quack token Secret. Returns
existingSecretKey when existingSecret is set, otherwise "quack-token" (the key
used in the chart-managed omneval-secret).

Usage: {{ include "omneval.quackSecretKey" . }}
*/ -}}
{{- define "omneval.quackSecretKey" -}}
{{- if .Values.quack.server.existingSecret -}}
{{- .Values.quack.server.existingSecretKey -}}
{{- else -}}
quack-token
{{- end }}
{{- end -}}

{{- /*
omneval.quackToken returns the Quack auth token shared between the
quack-server pod and its clients (Writer/Query/Eval).

Precedence:
1. existingSecret — when set, the token is sourced from the referenced Secret.
   The chart does NOT write quack-token into omneval-secret in this case.
2. token — when set to a non-empty string, used directly and written to
   omneval-secret. Stable across upgrades.
3. Auto-generated — when neither is set, the chart looks up the existing
   omneval-secret's quack-token value (stable across `helm upgrade`). On first
   install the secret does not exist yet, so a random token is generated and
   stored in the secret for subsequent upgrades.

Usage: {{ include "omneval.quackToken" . }}
*/ -}}
{{- define "omneval.quackToken" -}}
{{- if .Values.quack.server.token -}}
{{- .Values.quack.server.token -}}
{{- else -}}
{{- $secret := lookup "v1" "Secret" .Release.Namespace (printf "%s-secret" .Release.Name) -}}
{{- if and $secret (index $secret.data "quack-token") -}}
{{- index $secret.data "quack-token" | b64dec -}}
{{- else -}}
{{- randAlphaNum 32 -}}
{{- end }}
{{- end }}
{{- end -}}

{{- /*
omneval.quackHasExistingSecret returns true when existingSecret is set.

Usage: {{ include "omneval.quackHasExistingSecret" . }}
*/ -}}
{{- define "omneval.quackHasExistingSecret" -}}
{{- if .Values.quack.server.existingSecret -}}
true
{{- end }}
{{- end -}}

{{- /*
omneval.quackCatalogDSN returns the Quack Server's Catalog DSN. When
catalogDriver is "postgres" and no explicit catalogDSN is set, this derives
the same Postgres DSN as the metadata store. When catalogDriver is "duckdb",
this is a local catalog file path on the quack-server PVC.

Usage: {{ include "omneval.quackCatalogDSN" . }}
*/ -}}
{{- define "omneval.quackCatalogDSN" -}}
{{- if .Values.quack.server.catalogDSN -}}
{{- .Values.quack.server.catalogDSN -}}
{{- else if eq .Values.quack.server.catalogDriver "duckdb" -}}
/data/catalog.duckdb
{{- else -}}
{{- include "omneval.postgresDsn" . -}}
{{- end }}
{{- end -}}
