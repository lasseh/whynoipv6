const timeFormatter = new Intl.DateTimeFormat('en-GB', {
  hour: '2-digit',
  minute: '2-digit',
})

const dateFormatter = new Intl.DateTimeFormat('en-GB', {
  day: 'numeric',
  month: 'long',
  year: 'numeric',
})

/** "1 January 2022 12:30" — the site-wide timestamp format. */
export function formatDateTime(datetime: Date | string): string {
  const date = new Date(datetime)
  return `${dateFormatter.format(date)} ${timeFormatter.format(date)}`
}

/** "1 January 2022" — for date-only fields (history `day` values). */
export function formatDate(day: Date | string): string {
  return dateFormatter.format(new Date(day))
}

/** "12:30" — for rows already grouped under a day heading. */
export function formatTime(datetime: Date | string): string {
  return timeFormatter.format(new Date(datetime))
}
