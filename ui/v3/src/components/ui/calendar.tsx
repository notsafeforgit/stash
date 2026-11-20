import type * as React from "react";
import {
  Calendar as CalendarRoot,
  CalendarDay,
  CalendarGrid,
  CalendarHeading,
  CalendarMonthSelect,
  CalendarNext,
  CalendarPrevious,
  CalendarWeekday,
  CalendarWeekdays,
  CalendarWeeks,
  CalendarYearSelect,
} from "@ui-sdk/react/calendar";

type CaptionLayout = "label" | "dropdown";

/** The month grid's parts. Put it in a Calendar, or in a Date Picker's content. */
function CalendarPanel({
  captionLayout = "label",
}: {
  captionLayout?: CaptionLayout;
}) {
  return (
    <>
      <CalendarPrevious />
      {captionLayout === "dropdown" ? (
        <>
          <CalendarHeading className="sr-only" />
          <div data-slot="calendar-selects">
            <CalendarMonthSelect />
            <CalendarYearSelect />
          </div>
        </>
      ) : (
        <CalendarHeading />
      )}
      <CalendarNext />
      <CalendarGrid>
        <CalendarWeekdays>
          {(index) => <CalendarWeekday index={index} />}
        </CalendarWeekdays>
        <CalendarWeeks>
          {(cell) => <CalendarDay value={cell.date} />}
        </CalendarWeeks>
      </CalendarGrid>
    </>
  );
}

type CalendarProps = Omit<
  React.ComponentProps<typeof CalendarRoot>,
  "children"
> & {
  captionLayout?: CaptionLayout;
  children?: React.ReactNode;
};

function Calendar({ captionLayout, children, ...props }: CalendarProps) {
  return (
    <CalendarRoot {...props}>
      {children ?? <CalendarPanel captionLayout={captionLayout} />}
    </CalendarRoot>
  );
}

export { Calendar, CalendarPanel };
