{{/* Release-scoped base name: the release name when it already contains the chart name. */}}
{{- define "perspectiv.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 50 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 50 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "perspectiv.selectorLabels" -}}
app.kubernetes.io/name: {{ .Chart.Name }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "perspectiv.labels" -}}
{{ include "perspectiv.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version }}
{{- end -}}

{{- define "perspectiv.image" -}}
{{- $repo := required "image.repository is required" .Values.image.repository -}}
{{- if .Values.image.digest -}}
{{- printf "%s@%s" $repo .Values.image.digest -}}
{{- else -}}
{{- printf "%s:%s" $repo (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}
{{- end -}}

{{- define "perspectiv.host" -}}
{{- required "host is required" .Values.host -}}
{{- end -}}

{{- define "perspectiv.secret" -}}
{{- required "secrets.existingSecret is required" .Values.secrets.existingSecret -}}
{{- end -}}

{{/* The migration Job is named per release revision, so every install and upgrade migrates once and the pods wait for that run. */}}
{{- define "perspectiv.migrateJob" -}}
{{- printf "%s-migrate-%d" (include "perspectiv.fullname" .) (int .Release.Revision) -}}
{{- end -}}

{{- define "perspectiv.postgresHost" -}}
{{- printf "%s-postgres" (include "perspectiv.fullname" .) -}}
{{- end -}}

{{- define "perspectiv.waiterSA" -}}
{{- printf "%s-migration-waiter" (include "perspectiv.fullname" .) -}}
{{- end -}}

{{/*
Secret-backed env shared by the app containers and the migration Job.
POSTGRES_PASSWORD comes first: Kubernetes expands $(POSTGRES_PASSWORD) in
DATABASE_URL only from variables defined earlier in the list. The password is
generated as hex, so it needs no URL escaping. Postgres traffic stays inside
the cluster network on one node, behind a NetworkPolicy, so the connection is
unencrypted (sslmode=disable).
*/}}
{{- define "perspectiv.dbEnv" -}}
- name: POSTGRES_PASSWORD
  valueFrom:
    secretKeyRef:
      name: {{ include "perspectiv.secret" . }}
      key: POSTGRES_PASSWORD
- name: DATABASE_URL
  value: {{ printf "postgres://perspectiv:$(POSTGRES_PASSWORD)@%s:5432/perspectiv?sslmode=disable" (include "perspectiv.postgresHost" .) | quote }}
{{- end -}}

{{- define "perspectiv.appEnv" -}}
{{ include "perspectiv.dbEnv" . }}
- name: JWT_SECRET
  valueFrom:
    secretKeyRef:
      name: {{ include "perspectiv.secret" . }}
      key: JWT_SECRET
- name: ENCRYPTION_KEYS
  valueFrom:
    secretKeyRef:
      name: {{ include "perspectiv.secret" . }}
      key: ENCRYPTION_KEYS
- name: RESEND_API_KEY
  valueFrom:
    secretKeyRef:
      name: {{ include "perspectiv.secret" . }}
      key: RESEND_API_KEY
      optional: true
{{- end -}}

{{- define "perspectiv.containerSecurity" -}}
allowPrivilegeEscalation: false
readOnlyRootFilesystem: true
capabilities:
  drop: ["ALL"]
{{- end -}}

{{- define "perspectiv.podSecurity" -}}
runAsNonRoot: true
runAsUser: 65532
runAsGroup: 65532
fsGroup: 65532
seccompProfile:
  type: RuntimeDefault
{{- end -}}

{{/* Init container that holds a pod until this revision's migration Job has completed. */}}
{{- define "perspectiv.waitForMigration" -}}
- name: wait-for-migration
  image: {{ .Values.migrate.waitImage }}
  command: ["kubectl"]
  args:
    - wait
    - --for=condition=complete
    - --timeout={{ .Values.migrate.waitTimeout }}
    - job/{{ include "perspectiv.migrateJob" . }}
  env:
    - name: HOME
      value: /tmp
  securityContext:
    {{- include "perspectiv.containerSecurity" . | nindent 4 }}
  resources:
    requests: {cpu: 10m, memory: 32Mi}
    limits: {memory: 128Mi}
  volumeMounts:
    - name: tmp
      mountPath: /tmp
{{- end -}}
