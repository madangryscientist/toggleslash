# frozen_string_literal: true

require_relative 'context'
require_relative 'result'
require_relative 'validate'

module ToggleSlash
  module Eval
    # A segment that passed validation, with its include and exclude lists indexed by kind.
    ReadySegment = Struct.new(:segment, :included, :excluded)

    # A target that lists a key: its place in `targets` and the variation it serves.
    TargetHit = Struct.new(:order, :variation)

    # `status` is :malformed (not even a header), :broken (a header, but the body or a segment is bad; `code`
    # says which error) or :ready (`flag` is the flag, `targets` its index by kind then key).
    FlagEntry = Struct.new(:status, :header, :flag, :code, :targets)

    # The validated, indexed flags and segments. `segments` maps a key to a ReadySegment or :malformed.
    Ruleset = Struct.new(:flags, :segments)

    # Validates and indexes a ruleset (`{ "flags" => [...], "segments" => [...] }`, as the JSON parser hands
    # it over) once, so each evaluation skips that work. It never raises: a bad flag becomes an ERROR result
    # when it is evaluated.
    module Rules
      module_function

      def index_by_key(items)
        index = {}
        (items.is_a?(Array) ? items : []).each do |item|
          key = Validate.record?(item) ? item['key'] : nil
          index[key] = yield(item) if key.is_a?(String) && !index.key?(key)
        end
        index
      end

      def keys_by_kind(user_keys, lists)
        index = { DEFAULT_KIND => user_keys.to_h { |key| [key, true] } }
        (lists || []).each do |list|
          keys = (index[list['contextKind']] ||= {})
          list['values'].each { |value| keys[value] = true }
        end
        index
      end

      def compile_segment(value)
        return :malformed unless Validate.segment?(value)

        ReadySegment.new(value, keys_by_kind(value['included'], value['includedContexts']),
                         keys_by_kind(value['excluded'], value['excludedContexts']))
      end

      # Does the context have, in any of its kinds, a key that `keys` lists for that kind?
      def lists_context?(keys, contexts)
        contexts.any? { |kind, context| keys[kind]&.key?(context.key) }
      end

      # The first reference to a segment that is missing or malformed, in rule, clause, value order.
      def segment_problem(flag, segments)
        flag['rules'].each do |rule|
          rule['clauses'].each do |clause|
            next unless clause['op'] == 'segmentMatch'

            clause['values'].each do |key|
              segment = segments[key]
              return :SEGMENT_NOT_FOUND if segment.nil?
              return :MALFORMED_SEGMENT if segment == :malformed
            end
          end
        end
        nil
      end

      def target_index(flag)
        index = {}
        flag['targets'].each_with_index do |target, order|
          keys = (index[target['contextKind'] || DEFAULT_KIND] ||= {})
          variation = Validate.count?(target['variation']) ? target['variation'].to_i : 0
          target['contextKeys'].each { |key| keys[key] = TargetHit.new(order, variation) unless keys.key?(key) }
        end
        index
      end

      # The variation of the first target in array order that matches the context, or nil.
      def targeted_variation(targets, contexts)
        first = nil
        contexts.each do |kind, context|
          hit = targets[kind]&.[](context.key)
          first = hit if hit && (first.nil? || hit.order < first.order)
        end
        first&.variation
      end

      def compile_flag(value, segments)
        return FlagEntry.new(:malformed) unless Validate.flag_header?(value)

        count = value['variations'].length
        return FlagEntry.new(:broken, value, nil, :MALFORMED_FLAG) unless Validate.flag_body?(value, count)

        code = segment_problem(value, segments)
        return FlagEntry.new(:broken, value, nil, code) unless code.nil?

        FlagEntry.new(:ready, value, value, nil, target_index(value))
      end

      def build(input)
        flags = Validate.record?(input) ? input['flags'] : nil
        segment_items = Validate.record?(input) ? input['segments'] : nil
        segments = index_by_key(segment_items) { |item| compile_segment(item) }
        Ruleset.new(index_by_key(flags) { |item| compile_flag(item, segments) }, segments)
      end
    end
  end
end
