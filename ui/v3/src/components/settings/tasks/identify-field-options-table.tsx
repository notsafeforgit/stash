import { useIntl } from "react-intl";
import * as GQL from "src/core/generated-graphql";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "src/components/ui/select";
import { Checkbox } from "src/components/ui/checkbox";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "src/components/ui/table";
import {
  STRATEGIES,
  multiValueSceneFields,
  sceneFieldMessageID,
  sceneFields,
  strategyLabel,
  type SceneField,
} from "./identify-types";

/**
 * Three-state for createMissing: undefined = "use default" (only when
 * allowSetDefault), true/false otherwise. We render it as a checkbox in
 * the leaf "no inherited default" case and a select otherwise.
 */
interface IProps {
  fieldOptions: GQL.IdentifyFieldOptionsInput[];
  setFieldOptions: (opts: GQL.IdentifyFieldOptionsInput[]) => void;
  /** When true, "Use default" is a valid strategy choice (per-source case). */
  allowSetDefault: boolean;
  /** Default options whose strategy / createMissing are surfaced as "(default)". */
  defaultOptions?: GQL.IdentifyMetadataOptionsInput;
}

export function IdentifyFieldOptionsTable({
  fieldOptions,
  setFieldOptions,
  allowSetDefault,
  defaultOptions,
}: IProps) {
  const intl = useIntl();
  const useDefaultLabel = intl.formatMessage({
    id: "actions.use_default",
    defaultMessage: "Use default",
  });

  function findOption(field: SceneField) {
    return fieldOptions.find((f) => f.field === field);
  }

  function setStrategy(field: SceneField, strategy: string) {
    const next = fieldOptions.filter((f) => f.field !== field);
    if (strategy !== "__default__") {
      const prev = findOption(field);
      next.push({
        field,
        strategy: strategy as GQL.IdentifyFieldStrategy,
        createMissing: prev?.createMissing ?? false,
      });
    }
    setFieldOptions(next);
  }

  function setCreateMissing(field: SceneField, value: boolean) {
    const idx = fieldOptions.findIndex((f) => f.field === field);
    if (idx < 0) return;
    const next = [...fieldOptions];
    next[idx] = { ...next[idx], createMissing: value };
    setFieldOptions(next);
  }

  function defaultStrategyFor(
    field: SceneField,
  ): GQL.IdentifyFieldStrategy | undefined {
    return defaultOptions?.fieldOptions?.find((f) => f.field === field)
      ?.strategy;
  }

  function defaultCreateMissingFor(field: SceneField): boolean | undefined {
    const d = defaultOptions?.fieldOptions?.find((f) => f.field === field);
    return d?.createMissing ?? undefined;
  }

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>
            {intl.formatMessage({
              id: "config.tasks.identify.field",
              defaultMessage: "Field",
            })}
          </TableHead>
          <TableHead>
            {intl.formatMessage({
              id: "config.tasks.identify.strategy",
              defaultMessage: "Strategy",
            })}
          </TableHead>
          <TableHead>
            {intl.formatMessage({
              id: "config.tasks.identify.create_missing",
              defaultMessage: "Create missing",
            })}
          </TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sceneFields.map((field) => {
          const opt = findOption(field);
          const strategyValue =
            opt?.strategy ??
            (allowSetDefault
              ? "__default__"
              : (defaultStrategyFor(field) ?? GQL.IdentifyFieldStrategy.Merge));
          const isMulti = multiValueSceneFields.includes(field);
          const ignored = opt?.strategy === GQL.IdentifyFieldStrategy.Ignore;
          const usingDefault = strategyValue === "__default__";
          const inheritedDefault = defaultStrategyFor(field);

          return (
            <TableRow key={field}>
              <TableCell className="font-medium">
                {intl.formatMessage({ id: sceneFieldMessageID(field) })}
              </TableCell>
              <TableCell>
                <Select
                  value={strategyValue}
                  onValueChange={(v) => v && setStrategy(field, v)}
                >
                  <SelectTrigger className="w-36">
                    <SelectValue>
                      {usingDefault
                        ? `${useDefaultLabel}${
                            inheritedDefault
                              ? ` (${strategyLabel(inheritedDefault)})`
                              : ""
                          }`
                        : strategyLabel(
                            strategyValue as GQL.IdentifyFieldStrategy,
                          )}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {allowSetDefault && (
                      <SelectItem value="__default__">
                        {useDefaultLabel}
                        {inheritedDefault
                          ? ` (${strategyLabel(inheritedDefault)})`
                          : ""}
                      </SelectItem>
                    )}
                    {STRATEGIES.map((s) => (
                      <SelectItem key={s} value={s}>
                        {strategyLabel(s)}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </TableCell>
              <TableCell>
                {isMulti && !ignored && opt && (
                  <Checkbox
                    checked={opt.createMissing ?? false}
                    onCheckedChange={(v) => setCreateMissing(field, v === true)}
                  />
                )}
                {isMulti && !ignored && !opt && (
                  <span className="text-xs text-muted-foreground">
                    {useDefaultLabel}
                    {typeof defaultCreateMissingFor(field) === "boolean"
                      ? ` (${defaultCreateMissingFor(field) ? "yes" : "no"})`
                      : ""}
                  </span>
                )}
              </TableCell>
            </TableRow>
          );
        })}
      </TableBody>
    </Table>
  );
}
