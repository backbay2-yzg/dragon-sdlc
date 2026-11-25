import { useMemo, useState } from "react";
import {
  Badge,
  Menu,
  MenuDivider,
  MenuGroup,
  MenuGroupHeader,
  MenuItem,
  MenuList,
  MenuPopover,
  MenuTrigger,
} from "@fluentui/react-components";
import { ChevronDown12Regular } from "@fluentui/react-icons";
import type { ModelCatalogOption } from "../types/dragon.js";
import { BrandIcon } from "./BrandIcon.js";

interface ModelPickerPillProps {
  valueRef: string; // "providerId/modelId" or "opus"|"sonnet"|"haiku" or ""
  options: ModelCatalogOption[];
  onChange: (providerId: string, modelId: string, fullRef: string) => void;
  allowDefault?: boolean;
  defaultLabel?: string;
  includeTiers?: boolean;
  minWidth?: number;
}

const TIER_ITEMS = [
  { id: "opus", label: "Tier: Opus (旗舰推理)" },
  { id: "sonnet", label: "Tier: Sonnet (日常均衡)" },
  { id: "haiku", label: "Tier: Haiku (极速低成本)" },
];

export function ModelPickerPill({
  valueRef,
  options,
  onChange,
  allowDefault = true,
  defaultLabel = "CLI 默认模型",
  includeTiers = false,
  minWidth = 192,
}: ModelPickerPillProps) {
  const [open, setOpen] = useState<boolean>(false);

  const matchedOption = useMemo(() => {
    if (!valueRef) return undefined;
    return (
      options.find((o) => o.ref === valueRef) ||
      options.find((o) => o.modelId === valueRef)
    );
  }, [valueRef, options]);

  const grouped = useMemo(() => {
    const map = new Map<string, { providerName: string; providerId: string; hasKey: boolean; items: ModelCatalogOption[] }>();
    for (const opt of options) {
      let grp = map.get(opt.providerId);
      if (!grp) {
        grp = {
          providerId: opt.providerId,
          providerName: opt.providerName,
          hasKey: opt.hasKey,
          items: [],
        };
        map.set(opt.providerId, grp);
      }
      grp.items.push(opt);
    }
    return Array.from(map.values()).sort((a, b) => Number(b.hasKey) - Number(a.hasKey));
  }, [options]);

  const tierLabel = TIER_ITEMS.find((t) => t.id === valueRef)?.label;
  const displayTitle = tierLabel || matchedOption?.name || valueRef || defaultLabel;
  const iconKind = tierLabel
    ? "anthropic"
    : matchedOption?.providerId || (valueRef.includes("/") ? valueRef.split("/")[0] : "native");

  return (
    <Menu
      open={open}
      onOpenChange={(_, data) => setOpen(data.open)}
      positioning={{ position: "below", align: "end", autoSize: "height" }}
    >
      <MenuTrigger disableButtonEnhancement>
        <button type="button" className="dragon-model-pill" style={{ minWidth }}>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 7, minWidth: 0, flex: 1 }}>
            <BrandIcon kind={iconKind} size={18} />
            <span
              style={{
                overflow: "hidden",
                textOverflow: "ellipsis",
                whiteSpace: "nowrap",
                textAlign: "left",
              }}
            >
              {displayTitle}
            </span>
          </span>
          <ChevronDown12Regular style={{ color: "var(--dragon-text-secondary)", flexShrink: 0 }} />
        </button>
      </MenuTrigger>

      <MenuPopover style={{ maxHeight: 360, overflowY: "auto", minWidth: 250 }}>
        {open && (
          <MenuList>
            {allowDefault && (
              <>
                <MenuItem
                  icon={<BrandIcon kind="native" size={16} />}
                  onClick={() => onChange("", "", "")}
                >
                  {defaultLabel}
                </MenuItem>
                <MenuDivider />
              </>
            )}

            {includeTiers && (
              <>
                <MenuGroup>
                  <MenuGroupHeader>Claude Tiers</MenuGroupHeader>
                  {TIER_ITEMS.map((t) => (
                    <MenuItem
                      key={t.id}
                      icon={<BrandIcon kind="anthropic" size={16} />}
                      onClick={() => onChange("", t.id, t.id)}
                    >
                      {t.label}
                    </MenuItem>
                  ))}
                </MenuGroup>
                <MenuDivider />
              </>
            )}

            {grouped.map((grp) => (
              <MenuGroup key={grp.providerId}>
                <MenuGroupHeader>
                  <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
                    {grp.providerName}
                    {grp.hasKey && (
                      <Badge size="extra-small" appearance="tint" color="success">
                        已配 Key
                      </Badge>
                    )}
                  </span>
                </MenuGroupHeader>
                {grp.items.map((opt) => (
                  <MenuItem
                    key={opt.ref}
                    icon={<BrandIcon kind={opt.providerId} size={16} />}
                    secondaryContent={
                      <Badge
                        size="extra-small"
                        appearance="outline"
                        color={opt.costTier === "flagship" ? "brand" : opt.costTier === "budget" ? "success" : "informative"}
                      >
                        {opt.costTier}
                      </Badge>
                    }
                    onClick={() => onChange(opt.providerId, opt.modelId, opt.ref)}
                  >
                    {opt.name}
                  </MenuItem>
                ))}
              </MenuGroup>
            ))}
          </MenuList>
        )}
      </MenuPopover>
    </Menu>
  );
}
