# frozen_string_literal: true

module ToggleSlash
  module Eval
    # Instants, to the "Dates" section of docs/evaluation.md. A hand-written walk over the string: no regular
    # expression and no Time.parse. The result is Unix milliseconds, or nil when the value is not an instant.
    module Dates
      module_function

      # `length` ASCII digits at `start`, as an Integer; nil when any is not a digit (or the text is short).
      def digits(text, start, length)
        return nil if start + length > text.length

        value = 0
        length.times do |offset|
          byte = text.getbyte(start + offset)
          return nil unless byte.between?(0x30, 0x39)

          value = (value * 10) + (byte - 0x30)
        end
        value
      end

      def leap_year?(year)
        (year % 4).zero? && (!(year % 100).zero? || (year % 400).zero?)
      end

      def days_in_month(year, month)
        return leap_year?(year) ? 29 : 28 if month == 2

        [4, 6, 9, 11].include?(month) ? 30 : 31
      end

      # Days since 1970-01-01 in the proleptic Gregorian calendar ("days from civil").
      def days_from_civil(year, month, day)
        y = month <= 2 ? year - 1 : year
        era = y.div(400)
        year_of_era = y - (era * 400)
        month_index = month > 2 ? month - 3 : month + 9
        day_of_year = ((153 * month_index) + 2).div(5) + day - 1
        day_of_era = (year_of_era * 365) + year_of_era.div(4) - year_of_era.div(100) + day_of_year
        (era * 146_097) + day_of_era - 719_468
      end

      def parse_date(text)
        year = digits(text, 0, 4)
        month = digits(text, 5, 2)
        day = digits(text, 8, 2)
        return nil if year.nil? || month.nil? || day.nil?
        return nil if year < 1 || month < 1 || month > 12
        return nil if day < 1 || day > days_in_month(year, month)

        days_from_civil(year, month, day)
      end

      def parse_time(text, start)
        hour = digits(text, start, 2)
        minute = digits(text, start + 3, 2)
        second = digits(text, start + 6, 2)
        return nil if hour.nil? || minute.nil? || second.nil?
        return nil if hour > 23 || minute > 59 || second > 59

        (((hour * 60) + minute) * 60) + second
      end

      # The milliseconds of the fraction that starts at `start` (a `.`), and where it ends.
      def parse_fraction(text, start)
        finish = start + 1
        finish += 1 while finish < text.length && text.getbyte(finish).between?(0x30, 0x39)
        return nil if finish == start + 1

        kept = [finish - start - 1, 3].min
        scale = [1, 100, 10, 1][kept]
        [digits(text, start + 1, kept) * scale, finish]
      end

      # The offset `Z`, `+HH:MM` or `-HH:MM` that is all of `text` from `start`, in minutes east of UTC.
      def parse_offset(text, start)
        rest = text.byteslice(start..) || ''
        return 0 if rest == 'Z'
        return nil unless rest.bytesize == 6 && %w[+ -].include?(rest[0]) && rest[3] == ':'

        hour = digits(rest, 1, 2)
        minute = digits(rest, 4, 2)
        return nil if hour.nil? || minute.nil? || hour > 23 || minute > 59

        (rest[0] == '+' ? 1 : -1) * ((hour * 60) + minute)
      end

      def shape?(text)
        text.length >= 20 && text[4] == '-' && text[7] == '-' && text[10] == 'T' && text[13] == ':' && text[16] == ':'
      end

      # `YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)` as Unix milliseconds, or nil.
      def parse_instant(text)
        return nil unless text.ascii_only? && shape?(text)

        days = parse_date(text)
        seconds = parse_time(text, 11)
        return nil if days.nil? || seconds.nil?

        fraction = text[19] == '.' ? parse_fraction(text, 19) : [0, 19]
        return nil if fraction.nil?

        offset = parse_offset(text, fraction[1])
        return nil if offset.nil?

        (days * 86_400_000) + (seconds * 1000) + fraction[0] - (offset * 60_000)
      end

      # A finite number is Unix milliseconds, a string is parsed, anything else is not an instant.
      def to_instant(value)
        case value
        when Integer then value.to_f
        when Float then value.finite? ? value : nil
        when String then (instant = parse_instant(Text.utf8(value))) && instant.to_f
        end
      end
    end
  end
end
