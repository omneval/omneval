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
{{- else if .Values.minio.external.endpoint -}}
{{- .Values.minio.external.endpoint -}}
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
{{- else if .Values.minio.external.accessKey -}}
{{- .Values.minio.external.accessKey -}}
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
{{- else if .Values.minio.external.secretKey -}}
{{- .Values.minio.external.secretKey -}}
{{- end }}
{{- end -}}

{{- /*
omneval.storageBucket returns the S3 bucket name.

When MinIO is enabled internally, the bundled bucket-creation Job provisions a
bucket named "omneval", so that is used as the default unless storage.bucket is
explicitly set. For external storage, storage.bucket or minio.external.bucket
applies.

Usage: {{ include "omneval.storageBucket" . }}
*/ -}}
{{- define "omneval.storageBucket" -}}
{{- if .Values.storage.bucket -}}
{{- .Values.storage.bucket -}}
{{- else if .Values.minio.enabled -}}
omneval
{{- else if .Values.minio.external.bucket -}}
{{- .Values.minio.external.bucket -}}
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
