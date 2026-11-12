# frozen_string_literal: true

module Toggleslash
  module Eval
    # Instants: a hand-written walk over the bytes (evaluation.md, "Dates").
    module Dates
      def self.digits?(text)
        !text.empty? && text.bytes.all? { |b| b >= 48 && b <= 57 }
      end

      def self.leap?(year)
        (year % 4).zero? && (year % 100 != 0 || (year % 400).zero?)
      end

      def self.days_in_month(year, month)
        return leap?(year) ? 29 : 28 if month == 2

        [4, 6, 9, 11].include?(month) ? 30 : 31
      end

      def self.days_from_civil(year, month, day)
        y = month <= 2 ? year - 1 : year
        era = y / 400
        yoe = y - (era * 400)
        doy = (((153 * (month + (month > 2 ? -3 : 9))) + 2) / 5) + day - 1
        doe = (yoe * 365) + (yoe / 4) - (yoe / 100) + doy
        (era * 146_097) + doe - 719_468
      end

      # Unix milliseconds of a strict `YYYY-MM-DDTHH:MM:SS[.f+](Z|+HH:MM|-HH:MM)` string, or nil.
      def self.parse(text)
        return nil unless text.ascii_only? && text.bytesize >= 20

        b = text.bytes
        return nil unless b[4] == 45 && b[7] == 45 && b[10] == 84 && b[13] == 58 && b[16] == 58

        fields = [text[0, 4], text[5, 2], text[8, 2], text[11, 2], text[14, 2], text[17, 2]]
        return nil unless fields.all? { |f| digits?(f) }

        year, month, day, hour, minute, second = fields.map(&:to_i)
        return nil unless year.between?(1, 9999) && month.between?(1, 12)
        return nil unless day.between?(1, days_in_month(year, month))
        return nil if hour > 23 || minute > 59 || second > 59

        tail = zone(text, 19)
        return nil if tail.nil?

        fraction_ms, offset_minutes = tail
        millis = (days_from_civil(year, month, day) * 86_400_000) +
                 (((((hour * 60) + minute) * 60) + second) * 1000) + fraction_ms
        (millis - (offset_minutes * 60_000)).to_f
      end

      # Reads the optional fraction and the offset starting at `i`: [fraction_ms, offset_minutes] or nil.
      def self.zone(text, i)
        fraction_ms = 0
        if text[i] == "."
          j = i + 1
          j += 1 while j < text.size && text[j].ord >= 48 && text[j].ord <= 57
          digits = text[(i + 1)...j]
          return nil if digits.empty?

          fraction_ms = digits[0, 3].ljust(3, "0").to_i
          i = j
        end
        rest = text[i..]
        return [fraction_ms, 0] if rest == "Z"
        return nil unless rest.size == 6 && ["+", "-"].include?(rest[0]) && rest[3] == ":"
        return nil unless digits?(rest[1, 2]) && digits?(rest[4, 2])

        hours = rest[1, 2].to_i
        minutes = rest[4, 2].to_i
        return nil if hours > 23 || minutes > 59

        [fraction_ms, ((hours * 60) + minutes) * (rest[0] == "+" ? 1 : -1)]
      end

      # Unix milliseconds of a finite number or a valid date string; nil for anything else.
      def self.to_instant(value)
        case value
        when Float then value.finite? ? value : nil
        when String then parse(value)
        when Numeric then if Num.number?(value)
                            Num.to_float(value).then { |f| f.finite? ? f : nil }
                          end
        end
      end
    end
  end
end
