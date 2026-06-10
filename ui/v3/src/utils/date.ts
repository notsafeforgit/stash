import type { IntlShape } from "react-intl";

export function stringToDate(dateString: string): Date | null {
  if (!dateString) return null;

  const parts = dateString.split("-");
  if (parts.length !== 3) return null;

  const year = Number(parts[0]);
  const monthIndex = Math.max(0, Number(parts[1]) - 1);
  const day = Number(parts[2]);

  return new Date(year, monthIndex, day, 0, 0, 0, 0);
}

export function stringToFuzzyDate(dateString: string): Date | null {
  if (!dateString) return null;

  const parts = dateString.split("-");
  let year = Number(parts[0]);
  if (Number.isNaN(year)) year = new Date().getFullYear();
  let monthIndex = 0;
  if (parts.length > 1) {
    monthIndex = Math.max(0, Number(parts[1]) - 1);
    if (monthIndex > 11 || Number.isNaN(monthIndex)) monthIndex = 0;
  }
  let day = 1;
  if (parts.length > 2) {
    day = Number(parts[2]);
    if (day > 31 || Number.isNaN(day)) day = 1;
  }

  return new Date(year, monthIndex, day, 0, 0, 0, 0);
}

export function stringToFuzzyDateTime(dateString: string): Date | null {
  if (!dateString) return null;

  const dateTime = dateString.split(" ");

  let date: Date | null = null;
  if (dateTime.length > 0) {
    date = stringToFuzzyDate(dateTime[0]);
  }

  if (!date) {
    date = new Date();
  }

  if (dateTime.length > 1) {
    const timeParts = dateTime[1].split(":");
    if (date && timeParts.length > 0) {
      date.setHours(Number(timeParts[0]));
    }
    if (date && timeParts.length > 1) {
      date.setMinutes(Number(timeParts[1]));
    }
    if (date && timeParts.length > 2) {
      date.setSeconds(Number(timeParts[2]));
    }
  }

  return date;
}

export function dateToString(date: Date): string {
  return `${date.getFullYear()}-${(date.getMonth() + 1)
    .toString()
    .padStart(2, "0")}-${date.getDate().toString().padStart(2, "0")}`;
}

export function dateTimeToString(date: Date): string {
  return `${dateToString(date)} ${date
    .getHours()
    .toString()
    .padStart(2, "0")}:${date.getMinutes().toString().padStart(2, "0")}`;
}

export function getAge(
  dateString?: string | null,
  fromDateString?: string | null,
): number | null {
  if (!dateString) return null;

  const birthdate = stringToFuzzyDate(dateString);
  const fromDate = fromDateString
    ? stringToFuzzyDate(fromDateString)
    : new Date();

  if (!birthdate || !fromDate) return null;

  let age = fromDate.getFullYear() - birthdate.getFullYear();
  if (
    birthdate.getMonth() > fromDate.getMonth() ||
    (birthdate.getMonth() >= fromDate.getMonth() &&
      birthdate.getDate() > fromDate.getDate())
  ) {
    age -= 1;
  }

  return age;
}

export function formatDate(intl: IntlShape, date?: string, utc = true): string {
  if (!date) {
    return "";
  }

  return intl.formatDate(date, {
    format: "long",
    timeZone: utc ? "utc" : undefined,
  });
}

export function formatFuzzyDate(
  intl: IntlShape,
  date?: string,
  utc = true,
): string {
  if (!date) {
    return "";
  }

  const yearMatch = date.match(/^(\d{4})$/);
  if (yearMatch) {
    const year = parseInt(yearMatch[1], 10);
    return intl.formatDate(Date.UTC(year, 0), {
      year: "numeric",
      timeZone: utc ? "utc" : undefined,
    });
  }

  const yearMonthMatch = date.match(/^(\d{4})-(\d{2})$/);
  if (yearMonthMatch) {
    const year = parseInt(yearMonthMatch[1], 10);
    const month = parseInt(yearMonthMatch[2], 10) - 1;
    return intl.formatDate(Date.UTC(year, month), {
      year: "numeric",
      month: "long",
      timeZone: utc ? "utc" : undefined,
    });
  }

  return intl.formatDate(date, {
    format: "long",
    timeZone: utc ? "utc" : undefined,
  });
}

export function formatDateTime(
  intl: IntlShape,
  dateTime?: string,
  utc = false,
): string {
  return `${formatDate(intl, dateTime, utc)} ${intl.formatTime(dateTime, {
    timeZone: utc ? "utc" : undefined,
  })}`;
}
