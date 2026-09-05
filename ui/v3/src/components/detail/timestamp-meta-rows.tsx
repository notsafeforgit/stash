import { useIntl } from "react-intl";
import { MetaRow } from "src/components/detail/meta-row";
import { formatDateTime } from "src/utils/date";

type TimestampValue = string | null | undefined;

interface FileTimestamp {
  id?: string | null;
  path?: string | null;
  mod_time?: TimestampValue;
}

function basename(path: string): string {
  const parts = path.split(/[/\\]/).filter(Boolean);
  return parts.length > 0 ? parts[parts.length - 1] : path;
}

function TimestampText({ value }: { value: string }) {
  const intl = useIntl();
  return (
    <time dateTime={value} title={value}>
      {formatDateTime(intl, value)}
    </time>
  );
}

export function CreatedUpdatedMetaRows({
  createdAt,
  updatedAt,
}: {
  createdAt: TimestampValue;
  updatedAt: TimestampValue;
}) {
  const intl = useIntl();

  return (
    <>
      {createdAt && (
        <MetaRow
          label={intl.formatMessage({
            id: "created_at",
            defaultMessage: "Created at",
          })}
        >
          <TimestampText value={createdAt} />
        </MetaRow>
      )}
      {updatedAt && (
        <MetaRow
          label={intl.formatMessage({
            id: "updated_at",
            defaultMessage: "Updated at",
          })}
        >
          <TimestampText value={updatedAt} />
        </MetaRow>
      )}
    </>
  );
}

export function FileModTimeMetaRow({ modTime }: { modTime: TimestampValue }) {
  const intl = useIntl();
  if (!modTime) return null;

  return (
    <MetaRow
      label={intl.formatMessage({
        id: "file_mod_time",
        defaultMessage: "File modified",
      })}
    >
      <TimestampText value={modTime} />
    </MetaRow>
  );
}

export function FileModTimeMetaRows({
  files,
}: {
  files: readonly FileTimestamp[];
}) {
  const intl = useIntl();
  const filesWithModTime = files.filter(
    (file): file is FileTimestamp & { mod_time: string } => !!file.mod_time,
  );

  if (filesWithModTime.length === 0) return null;

  return (
    <MetaRow
      label={intl.formatMessage({
        id: "file_mod_time",
        defaultMessage: "File modified",
      })}
    >
      {filesWithModTime.length === 1 ? (
        <TimestampText value={filesWithModTime[0].mod_time} />
      ) : (
        <div className="flex flex-col gap-0.5">
          {filesWithModTime.map((file) => (
            <div
              key={file.id ?? file.path ?? file.mod_time}
              className="min-w-0"
            >
              {file.path && (
                <span
                  className="mr-1 text-muted-foreground"
                  title={file.path}
                  data-selectable-text
                >
                  {basename(file.path)}:
                </span>
              )}
              <TimestampText value={file.mod_time} />
            </div>
          ))}
        </div>
      )}
    </MetaRow>
  );
}
