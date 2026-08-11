"use client";

import { useState } from "react";
import { Eye, EyeOff } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";

type FieldProps = {
  data: Record<string, unknown>;
  onChange: (key: string, value: unknown) => void;
};

// ── 字段辅助组件 ──────────────────────────

function Field({ label, required, children }: { label: string; required?: boolean; children: React.ReactNode }) {
  return (
    <div className="space-y-1">
      <Label className="text-xs">{label}{required && <span className="text-destructive"> *</span>}</Label>
      {children}
    </div>
  );
}

function TextField({ label, field, placeholder, required, data, onChange }: { label: string; field: string; placeholder?: string; required?: boolean; data: Record<string, unknown>; onChange: (k: string, v: unknown) => void }) {
  return (
    <Field label={label} required={required}>
      <Input className="h-8 text-sm" placeholder={placeholder} value={String(data[field] ?? "")} onChange={(e) => onChange(field, e.target.value)} />
    </Field>
  );
}

function SecretField({ label, field, placeholder, required, data, onChange }: { label: string; field: string; placeholder?: string; required?: boolean; data: Record<string, unknown>; onChange: (k: string, v: unknown) => void }) {
  const [show, setShow] = useState(false);
  return (
    <Field label={label} required={required}>
      <div className="flex gap-1">
        <Input type={show ? "text" : "password"} className="h-8 font-mono text-xs" placeholder={placeholder} value={String(data[field] ?? "")} onChange={(e) => onChange(field, e.target.value)} />
        <Button type="button" size="icon" variant="ghost" className="size-8 shrink-0" onClick={() => setShow(!show)}>
          {show ? <EyeOff className="size-3" /> : <Eye className="size-3" />}
        </Button>
      </div>
    </Field>
  );
}

function BoolField({ label, field, data, onChange }: { label: string; field: string; data: Record<string, unknown>; onChange: (k: string, v: unknown) => void }) {
  return (
    <label className="flex cursor-pointer items-center gap-2.5 rounded-lg border px-3 py-2.5">
      <Checkbox checked={!!data[field]} onCheckedChange={(v) => onChange(field, v === true)} />
      <span className="text-xs">{label}</span>
    </label>
  );
}

// ── 各提供商字段 ──────────────────────────

function S3Fields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Endpoint" field="endpoint" placeholder="https://s3.amazonaws.com" data={data} onChange={onChange} />
      <TextField label="Region" field="region" placeholder="us-east-1" data={data} onChange={onChange} />
      <TextField label="Bucket" field="bucket" placeholder="my-bucket" required data={data} onChange={onChange} />
      <SecretField label="Access Key ID" field="access_key_id" placeholder="AKIA..." required data={data} onChange={onChange} />
      <SecretField label="Secret Access Key" field="secret_access_key" placeholder="wJalr..." required data={data} onChange={onChange} />
      <SecretField label="Session Token" field="session_token" placeholder="可选" data={data} onChange={onChange} />
      <BoolField label="使用 SSL" field="use_ssl" data={data} onChange={onChange} />
      <BoolField label="强制路径风格" field="force_path_style" data={data} onChange={onChange} />
    </div>
  );
}

function AliyunOSSFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Endpoint" field="endpoint" placeholder="oss-cn-hangzhou.aliyuncs.com" required data={data} onChange={onChange} />
      <TextField label="Bucket" field="bucket" placeholder="my-bucket" required data={data} onChange={onChange} />
      <SecretField label="AccessKey ID" field="access_key_id" placeholder="LTAI..." required data={data} onChange={onChange} />
      <SecretField label="AccessKey Secret" field="access_key_secret" placeholder="aBcD..." required data={data} onChange={onChange} />
    </div>
  );
}

function TencentCOSFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Endpoint" field="endpoint" placeholder="cos.ap-guangzhou.myqcloud.com" data={data} onChange={onChange} />
      <TextField label="Bucket URL" field="bucket_url" placeholder="https://bucket-xxx.cos.ap-guangzhou.myqcloud.com" data={data} onChange={onChange} />
      <SecretField label="Secret ID" field="secret_id" placeholder="AKIDz..." required data={data} onChange={onChange} />
      <SecretField label="Secret Key" field="secret_key" placeholder="bqyT..." required data={data} onChange={onChange} />
    </div>
  );
}

function QiniuKodoFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Region" field="region" placeholder="z0 (华东)" required data={data} onChange={onChange} />
      <TextField label="Bucket" field="bucket" placeholder="my-bucket" required data={data} onChange={onChange} />
      <SecretField label="Access Key" field="access_key" placeholder="Qiniu AccessKey" required data={data} onChange={onChange} />
      <SecretField label="Secret Key" field="secret_key" placeholder="Qiniu SecretKey" required data={data} onChange={onChange} />
      <TextField label="Domain" field="domain" placeholder="cdn.example.com" required data={data} onChange={onChange} />
      <BoolField label="使用 HTTPS" field="use_https" data={data} onChange={onChange} />
    </div>
  );
}

function WebDAVFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Endpoint" field="endpoint" placeholder="https://dav.example.com/remote.php/webdav/" required data={data} onChange={onChange} />
      <TextField label="Username" field="username" placeholder="用户名" data={data} onChange={onChange} />
      <SecretField label="Password" field="password" placeholder="密码" data={data} onChange={onChange} />
    </div>
  );
}

function OneDriveFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Drive ID" field="drive_id" placeholder="Drive ID" required data={data} onChange={onChange} />
      <SecretField label="Access Token" field="access_token" placeholder="eyJ0e..." data={data} onChange={onChange} />
      <SecretField label="Refresh Token" field="refresh_token" placeholder="OAQ..." data={data} onChange={onChange} />
      <TextField label="Client ID" field="client_id" placeholder="OAuth2 Client ID" data={data} onChange={onChange} />
      <SecretField label="Client Secret" field="client_secret" placeholder="OAuth2 Client Secret" data={data} onChange={onChange} />
      <TextField label="Tenant ID" field="tenant_id" placeholder="Azure Tenant ID" data={data} onChange={onChange} />
    </div>
  );
}

function DropboxFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <SecretField label="Access Token" field="access_token" placeholder="sl.B..." data={data} onChange={onChange} />
      <SecretField label="Refresh Token" field="refresh_token" placeholder="OAQ..." data={data} onChange={onChange} />
      <TextField label="App Key" field="app_key" placeholder="Dropbox App Key" data={data} onChange={onChange} />
      <SecretField label="App Secret" field="app_secret" placeholder="Dropbox App Secret" data={data} onChange={onChange} />
    </div>
  );
}

function GoogleDriveFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <SecretField label="Access Token" field="access_token" placeholder="ya29..." data={data} onChange={onChange} />
      <SecretField label="Refresh Token" field="refresh_token" placeholder="1//0..." data={data} onChange={onChange} />
      <TextField label="Client ID" field="client_id" placeholder="xxx.apps.googleusercontent.com" data={data} onChange={onChange} />
      <SecretField label="Client Secret" field="client_secret" placeholder="GOCSPX-..." data={data} onChange={onChange} />
      <TextField label="Drive ID" field="drive_id" placeholder="可选" data={data} onChange={onChange} />
      <TextField label="Folder ID" field="folder_id" placeholder="可选" data={data} onChange={onChange} />
    </div>
  );
}

function AzureBlobFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="Container" field="container" placeholder="my-container" required data={data} onChange={onChange} />
      <TextField label="Account URL" field="account_url" placeholder="https://account.blob.core.windows.net" data={data} onChange={onChange} />
      <TextField label="Account Name" field="account_name" placeholder="账户名" data={data} onChange={onChange} />
      <SecretField label="Account Key" field="account_key" placeholder="账户密钥" data={data} onChange={onChange} />
      <SecretField label="Connection String" field="connection_string" placeholder="DefaultEndpointsProtocol=https;..." data={data} onChange={onChange} />
    </div>
  );
}

function LocalFields({ data, onChange }: FieldProps) {
  return (
    <div className="grid gap-3 sm:grid-cols-2">
      <TextField label="存储根目录" field="root_dir" placeholder="data/storage（默认）" data={data} onChange={onChange} />
    </div>
  );
}

// ── 导出 ──────────────────────────

export function ProviderFields({ provider, data, onChange }: { provider: string } & FieldProps) {
  switch (provider) {
    case "s3": return <S3Fields data={data} onChange={onChange} />;
    case "aliyun_oss": return <AliyunOSSFields data={data} onChange={onChange} />;
    case "tencent_cos": return <TencentCOSFields data={data} onChange={onChange} />;
    case "qiniu_kodo": return <QiniuKodoFields data={data} onChange={onChange} />;
    case "webdav": return <WebDAVFields data={data} onChange={onChange} />;
    case "onedrive": return <OneDriveFields data={data} onChange={onChange} />;
    case "dropbox": return <DropboxFields data={data} onChange={onChange} />;
    case "google_drive": return <GoogleDriveFields data={data} onChange={onChange} />;
    case "azure_blob": return <AzureBlobFields data={data} onChange={onChange} />;
    case "local": return <LocalFields data={data} onChange={onChange} />;
    default: return <p className="text-xs text-muted-foreground">不支持的提供商: {provider}</p>;
  }
}

export const providerOptions = [
  { value: "s3", label: "Amazon S3 / S3 兼容" },
  { value: "aliyun_oss", label: "阿里云 OSS" },
  { value: "tencent_cos", label: "腾讯云 COS" },
  { value: "qiniu_kodo", label: "七牛云 Kodo" },
  { value: "webdav", label: "WebDAV" },
  { value: "onedrive", label: "OneDrive" },
  { value: "dropbox", label: "Dropbox" },
  { value: "google_drive", label: "Google Drive" },
  { value: "azure_blob", label: "Azure Blob" },
  { value: "local", label: "本地存储" }
];
