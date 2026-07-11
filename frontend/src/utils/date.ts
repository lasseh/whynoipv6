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
