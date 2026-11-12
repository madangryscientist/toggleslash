# frozen_string_literal: true

module Toggleslash
  module Eval
    # Numbers the evaluator sees: an Integer or a Float from JSON, and in a context any real Numeric of the
    # language (a Rational, a BigDecimal) as the JSON number of its value. Everything is compared as binary64.
    module Num
      TWO_53 = 9_007_199_254_740_992

      # True for a real number (a boolean is not one in Ruby; a Complex is not real).
      def self.number?(value)
        value.is_a?(Numeric) && value.real?
      end

      # The binary64 of a number; an Integer too large for it is an infinity. Never warns.
      def self.to_float(value)
        return value if value.is_a?(Float)
        return value.to_f unless value.is_a?(Integer)
        return value.to_f if value.abs <= TWO_53

        Rational(value).to_f
      end

      def self.finite?(value)
        to_float(value).finite?
      end

      # A non-negative whole number (an Integer, or a Float with no fraction).
      def self.count?(value)
        case value
        when Integer then value >= 0
        when Float then value.finite? && value == value.floor && value >= 0
        else false
        end
      end
    end
  end
end
