export function translateCron(cron: string): string {
  const clean = cron.trim()
  if (!clean) return 'Empty cron expression'

  // Presets mapping
  const presets: Record<string, string> = {
    '* * * * *': 'Runs every minute',
    '*/5 * * * *': 'Runs every 5 minutes',
    '*/15 * * * *': 'Runs every 15 minutes',
    '*/30 * * * *': 'Runs every 30 minutes',
    '0 * * * *': 'Runs every hour (at minute 0)',
    '0 0 * * *': 'Runs daily at midnight (00:00)',
    '0 0 * * 0': 'Runs weekly on Sundays at 00:00',
    '0 0 1 * *': 'Runs monthly on the 1st at 00:00'
  }

  if (presets[clean]) return presets[clean]

  const parts = clean.split(/\s+/)
  if (parts.length !== 5) {
    return 'Invalid cron expression (must be 5 fields)'
  }

  const [min, hour, dom, month, dow] = parts

  try {
    let minuteStr = ''
    if (min === '*') {
      minuteStr = 'every minute'
    } else if (min.startsWith('*/')) {
      const step = min.replace('*/', '')
      minuteStr = `every ${step} minutes`
    } else if (/^\d+$/.test(min)) {
      minuteStr = `at minute ${min}`
    } else {
      minuteStr = `at minutes (${min})`
    }

    let hourStr = ''
    if (hour === '*') {
      hourStr = 'of every hour'
    } else if (hour.startsWith('*/')) {
      const step = hour.replace('*/', '')
      hourStr = `of every ${step} hours`
    } else if (/^\d+$/.test(hour)) {
      hourStr = `at hour ${hour.padStart(2, '0')}:XX`
    } else {
      hourStr = `at hours (${hour})`
    }

    // Specific formatting for "at hour HH:MM"
    let timeStr = ''
    if (/^\d+$/.test(min) && /^\d+$/.test(hour)) {
      const hVal = hour.padStart(2, '0')
      const mVal = min.padStart(2, '0')
      timeStr = `at ${hVal}:${mVal}`
    }

    let domStr = ''
    if (dom !== '*') {
      if (/^\d+$/.test(dom)) {
        domStr = `on day ${dom} of the month`
      } else {
        domStr = `on days (${dom}) of the month`
      }
    }

    let monthStr = ''
    if (month !== '*') {
      const monthNames = ['', 'January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
      if (/^\d+$/.test(month)) {
        const mIdx = parseInt(month, 10)
        if (mIdx >= 1 && mIdx <= 12) {
          monthStr = `in ${monthNames[mIdx]}`
        } else {
          monthStr = `in month ${month}`
        }
      } else {
        monthStr = `in months (${month})`
      }
    }

    let dowStr = ''
    if (dow !== '*') {
      const dowNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday']
      if (/^\d+$/.test(dow)) {
        const dIdx = parseInt(dow, 10)
        if (dIdx >= 0 && dIdx <= 7) {
          dowStr = `on ${dowNames[dIdx]}`
        } else {
          dowStr = `on weekday ${dow}`
        }
      } else {
        dowStr = `on weekdays (${dow})`
      }
    }

    // Assemble description
    if (timeStr) {
      const components = [
        `Runs ${timeStr}`,
        dowStr,
        domStr,
        monthStr
      ].filter(Boolean)
      return components.join(' ')
    } else {
      const components = [
        'Runs',
        minuteStr,
        hourStr,
        dowStr,
        domStr,
        monthStr
      ].filter(Boolean)
      return components.join(' ')
    }
  } catch (e) {
    return 'Invalid cron expression format'
  }
}
