/**
 * Curated stable UI primitives exposed to v3 plugins.
 *
 * The signatures of these exports are part of the host major-version
 * contract — they will not break within host v1. Don't expand the set
 * casually; once an export is here, it's stuck here for the v1
 * lifetime. Additions are compatible within a major version.
 */

export { Button, buttonVariants } from "src/components/ui/button";
export {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from "src/components/ui/card";
export { Checkbox } from "src/components/ui/checkbox";
export {
  Combobox,
  ComboboxContent,
  ComboboxEmpty,
  ComboboxInput,
  ComboboxItem,
  ComboboxList,
  ComboboxTrigger,
  ComboboxValue,
} from "src/components/ui/combobox";
export {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "src/components/ui/dialog";
export {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "src/components/ui/dropdown-menu";
export { Input } from "src/components/ui/input";
export { Label } from "src/components/ui/label";
export {
  Field,
  FieldContent,
  FieldDescription,
  FieldError,
  FieldGroup,
  FieldLabel,
  FieldLegend,
  FieldSeparator,
  FieldSet,
  FieldTitle,
} from "src/components/ui/field";
export { Alert, AlertTitle, AlertDescription } from "src/components/ui/alert";
export { Badge } from "src/components/ui/badge";
export {
  Tabs,
  TabsList,
  TabsTrigger,
  TabsContent,
} from "src/components/ui/tabs";
export {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
export {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "src/components/ui/sheet";
export { Spinner } from "src/components/ui/spinner";
export { Textarea } from "src/components/ui/textarea";
export {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "src/components/ui/tooltip";

import type * as ui from "./ui-exports";
export type StashPluginUI = typeof ui;
