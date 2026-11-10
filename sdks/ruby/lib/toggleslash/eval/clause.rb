# frozen_string_literal: true

require_relative 'context'
require_relative 'dates'
require_relative 'pattern'
require_relative 'reference'
require_relative 'semver'
require_relative 'text'
require_relative 'validate'

module ToggleSlash
  module Eval
    # Does one attribute clause hold: to "Clauses" in docs/evaluation.md.
    module Clause
      STRING_OPS = %w[contains startsWith endsWith matches].freeze
      SEMVER_OPS = %w[semverEq semverLt semverLte semverGt semverGte].freeze

      module_function

      # A property of a Hash by its own name (a String key; a Symbol key too, for a context a caller built).
      def own_value(source, name)
        return source[name] if source.key?(name)
        return nil unless name.valid_encoding?

        source.fetch(name.to_sym, nil)
      end

      # The value a name or a `/` reference reaches, or nil when it is missing.
      def read_attribute(context, name)
        components = name.start_with?('/') ? Reference.parse(name) : [name]
        first, *rest = components || []
        return nil if first.nil?

        current = nil
        if first == 'key' then current = context.key
        elsif context.attributes then current = own_value(context.attributes, first)
        end
        rest.each do |component|
          return nil unless Validate.record?(current)

          current = own_value(current, component)
        end
        current
      end

      def number?(value)
        value.is_a?(Integer) || value.is_a?(Float)
      end

      # -1, 0 or 1 for two finite numbers or two strings (by UTF-16 code unit); otherwise nil.
      def compare(attribute, value)
        if number?(attribute) && number?(value)
          left = attribute.to_f
          return left.finite? ? (left <=> value.to_f) : nil
        end
        return Text.compare(Text.utf8(attribute), value) if attribute.is_a?(String) && value.is_a?(String)

        nil
      end

      def matches_pattern?(attribute, value)
        program = Pattern.compile(value)
        !program.nil? && Pattern.matches?(program, attribute)
      end

      def matches_string?(operator, attribute, value)
        case operator
        when 'matches' then matches_pattern?(attribute, value)
        when 'contains' then Text.include?(attribute, value)
        when 'startsWith' then Text.start_with?(attribute, value)
        else Text.end_with?(attribute, value)
        end
      end

      def matches_order?(operator, order)
        case operator
        when 'lt' then order.negative?
        when 'lte' then order <= 0
        when 'gt' then order.positive?
        else order >= 0
        end
      end

      def matches_semver?(operator, attribute, value)
        return false unless attribute.is_a?(String) && value.is_a?(String)

        candidate = Semver.parse(attribute)
        target = Semver.parse(value)
        return false if candidate.nil? || target.nil?

        order = Semver.compare(candidate, target)
        case operator
        when 'semverEq' then order.zero?
        when 'semverLt' then order.negative?
        when 'semverLte' then order <= 0
        when 'semverGt' then order.positive?
        else order >= 0
        end
      end

      def matches_date?(operator, attribute, value)
        candidate = Dates.to_instant(attribute)
        target = Dates.to_instant(value)
        return false if candidate.nil? || target.nil?

        operator == 'before' ? candidate < target : candidate > target
      end

      def matches_value?(operator, attribute, value)
        if operator == 'in'
          return attribute == value if [true, false].include?(attribute)

          return compare(attribute, value) == 0 # rubocop:disable Style/NumericPredicate
        end
        if STRING_OPS.include?(operator)
          return attribute.is_a?(String) && value.is_a?(String) &&
                 matches_string?(operator, Text.utf8(attribute), Text.utf8(value))
        end
        return matches_semver?(operator, attribute, value) if SEMVER_OPS.include?(operator)
        return matches_date?(operator, attribute, value) if %w[before after].include?(operator)

        order = compare(attribute, value)
        !order.nil? && matches_order?(operator, order)
      end

      # Does one attribute clause hold? It reads the context of its `contextKind`. A missing attribute, or a
      # missing kind, fails it, whatever `negate` says.
      def attribute_matches?(clause, contexts)
        context = contexts[clause['contextKind'] || DEFAULT_KIND]
        return false if context.nil?

        attribute = read_attribute(context, clause['attribute'])
        return false if attribute.nil?

        candidates = attribute.is_a?(Array) ? attribute : [attribute]
        operator = clause['op']
        matched = candidates.any? do |candidate|
          clause['values'].any? { |value| matches_value?(operator, candidate, value) }
        end
        clause['negate'] == true ? !matched : matched
      end
    end
  end
end
