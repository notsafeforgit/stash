import { GalleryHorizontalEnd } from "lucide-react";
import { useMsg } from "@/hooks/message";
import {
  useConfigurationContextOptional,
  useConfigureUISetting,
} from "@/hooks/config";
import { Button } from "@/components/ui/button";
import { SettingSwitch } from "@/components/settings/setting-row";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuCheckboxItem,
} from "@/components/ui/dropdown-menu";

export function GalleryLightboxOptions({
  inline = false,
}: {
  inline?: boolean;
}) {
  const msg = useMsg();
  const config = useConfigurationContextOptional();
  const [save, { loading }] = useConfigureUISetting();
  if (!config) return null;
  const enabled = config.configuration.ui.continueGalleryLightbox ?? false;
  const label = msg(
    "config.ui.continue_gallery_lightbox.heading",
    "Continue to next gallery",
  );
  const change = (value: boolean) => {
    void save({ variables: { key: "continueGalleryLightbox", value } });
  };
  if (inline)
    return (
      <div data-base-ui-swipe-ignore="">
        <SettingSwitch
          label={label}
          description={msg(
            "config.ui.continue_gallery_lightbox.description",
            "Swipe through galleries in the current list's filter and sort order, then wrap to the beginning. When off, loop within the opened gallery.",
          )}
          checked={enabled}
          disabled={loading}
          onChange={change}
        />
      </div>
    );
  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <Button
            variant={enabled ? "secondary" : "ghost"}
            size="icon-sm"
            aria-label={msg(
              "config.ui.continue_gallery_lightbox.options",
              "Gallery viewer options",
            )}
          />
        }
      >
        <GalleryHorizontalEnd />
      </DropdownMenuTrigger>
      <DropdownMenuContent>
        <DropdownMenuGroup>
          <DropdownMenuCheckboxItem
            checked={enabled}
            disabled={loading}
            onCheckedChange={change}
          >
            {label}
          </DropdownMenuCheckboxItem>
        </DropdownMenuGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
