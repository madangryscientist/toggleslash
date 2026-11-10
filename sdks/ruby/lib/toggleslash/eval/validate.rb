# frozen_string_literal: true

require_relative 'bucket'
require_relative 'reference'

module ToggleSlash
  module Eval
    # The shape of a ruleset, checked at runtime because rulesets arrive as JSON. docs/evaluation.md is
    # normative for what counts as malformed. Null and absent are the same for an optional field. No
    # regular expression is used anywhere in the evaluator: every format is walked byte by byte.
    module Validate
      ATTRIBUTE_OPS = %w[
        in contains startsWith endsWith lt lte gt gte semverEq semverLt semverLte semverGt semverGte
        before after matches
      ].freeze

      module_function

      def record?(value)
        value.is_a?(Hash)
      end

      def absent?(value)
        value.nil?
      end

      # `^[A-Za-z0-9._-]+$`, and neither `kind` nor `multi`.
      def kind?(value)
        return false unless value.is_a?(String) && !value.empty? && value != 'kind' && value != 'multi'

        value.each_byte.all? do |byte|
          byte.between?(0x30, 0x39) || byte.between?(0x41, 0x5A) || byte.between?(0x61, 0x7A) ||
            [0x2E, 0x5F, 0x2D].include?(byte)
        end
      end

      def optional_kind?(value)
        absent?(value) || kind?(value)
      end

      def key?(value)
        value.is_a?(String) && !value.empty?
      end

      # A non-negative whole number (JSON has one number type, so `2.0` is a count too).
      def count?(value)
        case value
        when Integer then value >= 0
        when Float then value.finite? && value >= 0 && value == value.floor
        else false
        end
      end

      def variation_index?(value, variation_count)
        count?(value) && value < variation_count
      end

      def string_array?(value)
        value.is_a?(Array) && value.all?(String)
      end

      def clause_value?(value)
        case value
        when Integer, String, true, false then true
        when Float then value.finite?
        else false
        end
      end

      def negate?(value)
        absent?(value) || value == true || value == false
      end

      def attribute_name?(value)
        return false unless key?(value)

        !value.start_with?('/') || !Reference.parse(value).nil?
      end

      def every?(value, &)
        value.is_a?(Array) && value.all?(&)
      end

      def attribute_clause?(value)
        return false unless record?(value) && ATTRIBUTE_OPS.include?(value['op'])

        attribute_name?(value['attribute']) && negate?(value['negate']) &&
          optional_kind?(value['contextKind']) && every?(value['values']) { |item| clause_value?(item) }
      end

      def segment_clause?(value)
        record?(value) && value['op'] == 'segmentMatch' && negate?(value['negate']) && string_array?(value['values'])
      end

      def weighted_variation?(value, variation_count)
        record?(value) && variation_index?(value['variation'], variation_count) &&
          count?(value['weight']) && value['weight'] <= BUCKET_COUNT
      end

      def rollout?(value, variation_count)
        return false unless record?(value) && optional_kind?(value['contextKind'])

        variations = value['variations']
        return false unless variations.is_a?(Array) && !variations.empty?
        return false unless variations.all? { |item| weighted_variation?(item, variation_count) }

        variations.sum { |item| item['weight'] } == BUCKET_COUNT
      end

      def serve?(value, variation_count)
        return false unless record?(value)

        variation = value['variation']
        rollout = value['rollout']
        return variation_index?(variation, variation_count) if absent?(rollout)

        absent?(variation) && rollout?(rollout, variation_count)
      end

      def rule?(value, variation_count)
        return false unless record?(value)

        every?(value['clauses']) { |clause| attribute_clause?(clause) || segment_clause?(clause) } &&
          serve?(value, variation_count)
      end

      def target?(value, variation_count)
        record?(value) && variation_index?(value['variation'], variation_count) &&
          string_array?(value['contextKeys']) && optional_kind?(value['contextKind'])
      end

      def prerequisite?(value)
        record?(value) && key?(value['flagKey']) && count?(value['variation'])
      end

      # What serving the off variation needs. It is checked apart so `on: false` survives a bad rule.
      def flag_header?(value)
        return false unless record?(value) && [true, false].include?(value['on'])

        variations = value['variations']
        return false unless key?(value['key']) && variations.is_a?(Array) && !variations.empty?

        off = value['offVariation']
        absent?(off) || variation_index?(off, variations.length)
      end

      def flag_body?(value, variation_count)
        record?(value) && value['salt'].is_a?(String) && count?(value['version']) &&
          every?(value['targets']) { |item| target?(item, variation_count) } &&
          every?(value['prerequisites']) { |item| prerequisite?(item) } &&
          every?(value['rules']) { |item| rule?(item, variation_count) } &&
          serve?(value['fallthrough'], variation_count)
      end

      def segment_rule?(value)
        return false unless record?(value)

        weight = value['weight']
        (absent?(weight) || (count?(weight) && weight <= BUCKET_COUNT)) &&
          optional_kind?(value['rolloutContextKind']) &&
          every?(value['clauses']) { |clause| attribute_clause?(clause) }
      end

      def context_list?(value)
        record?(value) && kind?(value['contextKind']) && string_array?(value['values'])
      end

      def optional_context_lists?(value)
        absent?(value) || every?(value) { |item| context_list?(item) }
      end

      def segment?(value)
        record?(value) && key?(value['key']) && value['salt'].is_a?(String) &&
          string_array?(value['included']) && string_array?(value['excluded']) &&
          optional_context_lists?(value['includedContexts']) &&
          optional_context_lists?(value['excludedContexts']) &&
          every?(value['rules']) { |rule| segment_rule?(rule) }
      end
    end
  end
end
