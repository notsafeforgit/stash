export const sanitiseURL = (url?: string, siteURL?: URL) => {
  if (!url) {
    return url;
  }

  if (url.startsWith("http://") || url.startsWith("https://")) {
    return url;
  }

  if (siteURL) {
    if (url.startsWith(siteURL.host)) {
      return `${siteURL.protocol}//${url}`;
    }

    return `${siteURL.protocol}//${siteURL.host}/${url}`;
  }

  return `https://${url}`;
};

export const domainFromURL = (urlString?: string, url?: URL) => {
  if (url) {
    return url.hostname;
  } else if (urlString) {
    let urlDomain = "";
    try {
      const sanitizedUrl = sanitiseURL(urlString) ?? urlString;
      urlDomain = new URL(sanitizedUrl).hostname;
    } catch {
      urlDomain = urlString;
    }
    return urlDomain;
  } else {
    return "";
  }
};

type CountUnit = "" | "K" | "M" | "B";
const CountUnits: CountUnit[] = ["", "K", "M", "B"];

export const abbreviateCounter = (counter: number = 0) => {
  if (Number.isNaN(parseFloat(String(counter))) || !Number.isFinite(counter))
    return { size: 0, unit: CountUnits[0] };

  let unit = 0;
  let digits = 0;
  let count = counter;
  while (count >= 1000 && unit + 1 < CountUnits.length) {
    count /= 1000;
    unit++;
    digits = 1;
  }

  return { size: count, unit: CountUnits[unit], digits };
};

/*
 * Trims quotes if the text has leading/trailing quotes
 */
export const stripQuotes = (text: string) => {
  if (text.startsWith('"') && text.endsWith('"')) return text.slice(1, -1);
  return text;
};

/*
 * Wraps string in quotes
 */
export const addQuotes = (text: string) => `"${text}"`;
