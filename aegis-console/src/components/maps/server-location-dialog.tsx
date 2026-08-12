"use client";

// 服务端端点位置的编辑弹窗（攻击飞线图 / 用户活动地图共用）。
//
// 表单状态在 DialogContent 内部的子组件里，靠 Radix 关闭时卸载来复位 ——
// 不用 useEffect 把 props 同步进 state（项目通用约束）。

import { useState } from "react";
import { MapPin } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { saveServerLocation, type ServerLocation } from "@/lib/geo/server-location";

type Props = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  value: ServerLocation;
  /** 说明这张图里「端点」意味着什么（两张图的语义不同） */
  description: string;
  /** 保存后回调，通常用来重新取景 */
  onSaved?: (next: ServerLocation) => void;
};

export function ServerLocationDialog({ open, onOpenChange, value, description, onSaved }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2 text-sm">
            <MapPin className="size-4 text-violet-500" />
            服务器端点位置
          </DialogTitle>
          <DialogDescription className="text-xs">{description}</DialogDescription>
        </DialogHeader>
        <LocationForm
          value={value}
          onCancel={() => onOpenChange(false)}
          onSave={(next) => {
            saveServerLocation(next);
            onOpenChange(false);
            onSaved?.(next);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

function LocationForm({
  value,
  onCancel,
  onSave
}: {
  value: ServerLocation;
  onCancel: () => void;
  onSave: (next: ServerLocation) => void;
}) {
  const [name, setName] = useState(value.name);
  const [lat, setLat] = useState(String(value.lat));
  const [lng, setLng] = useState(String(value.lng));

  const latNum = Number(lat);
  const lngNum = Number(lng);
  const latOk = Number.isFinite(latNum) && latNum >= -90 && latNum <= 90;
  const lngOk = Number.isFinite(lngNum) && lngNum >= -180 && lngNum <= 180;

  return (
    <>
      <div className="space-y-3">
        <div className="space-y-1.5">
          <Label className="text-[12px]">名称</Label>
          <Input className="h-9 text-sm" value={name} onChange={(e) => setName(e.target.value)} />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div className="space-y-1.5">
            <Label className="text-[12px]">纬度</Label>
            <Input
              className="h-9 font-mono text-sm"
              value={lat}
              onChange={(e) => setLat(e.target.value)}
              placeholder="-90 ~ 90"
              aria-invalid={!latOk}
            />
          </div>
          <div className="space-y-1.5">
            <Label className="text-[12px]">经度</Label>
            <Input
              className="h-9 font-mono text-sm"
              value={lng}
              onChange={(e) => setLng(e.target.value)}
              placeholder="-180 ~ 180"
              aria-invalid={!lngOk}
            />
          </div>
        </div>
        <p className="text-[11px] leading-4 text-muted-foreground">
          该位置在「攻击飞线图」与「用户活动地图」之间共享，改一处两处都会变。
        </p>
      </div>
      <DialogFooter>
        <Button variant="ghost" size="sm" onClick={onCancel}>
          取消
        </Button>
        <Button
          size="sm"
          disabled={!latOk || !lngOk}
          onClick={() => onSave({ name: name.trim() || "服务器", lat: latNum, lng: lngNum })}
        >
          保存
        </Button>
      </DialogFooter>
    </>
  );
}
