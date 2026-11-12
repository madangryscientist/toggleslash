# frozen_string_literal: true

module Toggleslash
  module Eval
    BUCKET_COUNT = 100_000
    ATTRIBUTE_OPS = %w[
      in contains startsWith endsWith lt lte gt gte semverEq semverLt semverLte semverGt semverGte
      before after matches
    ].freeze

    Clause = Struct.new(:path, :kind, :op, :operands, :negate, :segments)
    Serve = Struct.new(:variation, :kind, :weights)
    Rule = Struct.new(:clauses, :serve)
    Flag = Struct.new(:key, :salt, :version, :on, :variations, :off, :targets, :prerequisites, :rules,
                      :fallthrough)
    # status is "malformed", "broken" (with a code) or "ready".
    Entry = Struct.new(:status, :flag, :code)
    SegmentRule = Struct.new(:clauses, :weight, :kind)
    Segment = Struct.new(:key, :salt, :included, :excluded, :rules)

    # A loaded ruleset: never changed after it is built, so evaluations on many threads never interfere.
    # The variation values it hands out are frozen.
    class Ruleset
      attr_reader :flags, :segments

      def initialize(flags, segments)
        @flags = flags
        @segments = segments
        freeze
      end

      # The `version` of a flag that is fully valid, or nil.
      def flag_version(key)
        entry = @flags[key]
        return nil if entry.nil? || entry.status != "ready" || entry.flag.nil?

        entry.flag.version
      end

      def size
        @flags.size
      end
    end

    # Loading: checks every flag and segment against evaluation.md. Never raises; a bad entry becomes an
    # ERROR when it is evaluated.
    module Loader
      class Invalid < StandardError; end

      module_function

      def object?(value)
        value.is_a?(Hash)
      end

      def key?(value)
        value.is_a?(String) && !value.empty?
      end

      def string_list?(value)
        value.is_a?(Array) && value.all?(String)
      end

      def optional_kind(value)
        return Context::DEFAULT_KIND if value.nil?
        raise Invalid unless Context.kind?(value)

        value
      end

      def index(value, count)
        raise Invalid if !Num.count?(value) || value >= count

        value.to_i
      end

      def clause_value_ok?(value)
        value == true || value == false || value.is_a?(String) || (Num.number?(value) && Num.finite?(value))
      end

      def attribute_clause(raw)
        raise Invalid unless object?(raw) && ATTRIBUTE_OPS.include?(raw["op"])

        negate = raw["negate"]
        raise Invalid unless negate.nil? || negate == true || negate == false

        path = Reference.path(raw["attribute"])
        raise Invalid if path.nil?

        values = raw["values"]
        raise Invalid unless values.is_a?(Array) && values.all? { |v| clause_value_ok?(v) }

        Clause.new(path, optional_kind(raw["contextKind"]), raw["op"], values, negate == true, nil)
      end

      def segment_clause(raw)
        negate = raw["negate"]
        values = raw["values"]
        raise Invalid unless (negate.nil? || negate == true || negate == false) && string_list?(values)

        Clause.new(nil, Context::DEFAULT_KIND, "segmentMatch", [], negate == true, values)
      end

      def rule_clause(raw)
        raise Invalid unless object?(raw)

        raw["op"] == "segmentMatch" ? segment_clause(raw) : attribute_clause(raw)
      end

      def serve(raw, count)
        raise Invalid unless object?(raw)

        rollout = raw["rollout"]
        variation = raw["variation"]
        return Serve.new(index(variation, count), Context::DEFAULT_KIND, []) if rollout.nil?
        raise Invalid unless variation.nil? && object?(rollout)

        kind = optional_kind(rollout["contextKind"])
        items = rollout["variations"]
        raise Invalid unless items.is_a?(Array) && !items.empty?

        entries = []
        total = 0
        items.each do |item|
          raise Invalid unless object?(item)

          weight = item["weight"]
          raise Invalid if !Num.count?(weight) || weight > BUCKET_COUNT

          entries << [index(item["variation"], count), weight.to_i]
          total += weight.to_i
        end
        raise Invalid unless total == BUCKET_COUNT

        Serve.new(nil, kind, entries)
      end

      def rule(raw, count)
        raise Invalid unless object?(raw)

        clauses = raw["clauses"]
        raise Invalid unless clauses.is_a?(Array)

        Rule.new(clauses.map { |c| rule_clause(c) }, serve(raw, count))
      end

      def header_ok?(raw)
        return false unless object?(raw) && [true, false].include?(raw["on"])

        variations = raw["variations"]
        return false unless key?(raw["key"]) && variations.is_a?(Array) && !variations.empty?

        off = raw["offVariation"]
        off.nil? || (Num.count?(off) && off < variations.size)
      end

      def targets(list, count)
        out = {}
        list.each_with_index do |target, order|
          raise Invalid unless object?(target)

          variation = index(target["variation"], count)
          keys = target["contextKeys"]
          raise Invalid unless string_list?(keys)

          by_key = (out[optional_kind(target["contextKind"])] ||= {})
          keys.each { |k| by_key[k] = [variation, order] unless by_key.key?(k) }
        end
        out
      end

      def prerequisites(list)
        list.map do |item|
          raise Invalid unless object?(item) && key?(item["flagKey"]) && Num.count?(item["variation"])

          [item["flagKey"], item["variation"].to_i]
        end
      end

      # Freezes a parsed JSON value and everything in it, without recursion.
      def deep_freeze(value)
        pending = [value]
        until pending.empty?
          current = pending.pop
          next if current.frozen? && !current.is_a?(Array) && !current.is_a?(Hash)

          case current
          when Array then pending.concat(current)
          when Hash
            pending.concat(current.keys)
            pending.concat(current.values)
          end
          current.freeze
        end
        value
      end

      def flag(raw)
        return Entry.new("malformed", nil, ErrorCode::MALFORMED_FLAG) unless header_ok?(raw)

        deep_freeze(raw["variations"])

        flag = Flag.new(raw["key"], "", 0, raw["on"], raw["variations"], nil, {}, [], [],
                        Serve.new(0, Context::DEFAULT_KIND, []))
        off = raw["offVariation"]
        flag.off = off&.to_i
        begin
          fill_flag(flag, raw)
        rescue Invalid
          return Entry.new("broken", flag, ErrorCode::MALFORMED_FLAG)
        end
        Entry.new("ready", flag, nil)
      end

      def fill_flag(flag, raw)
        count = flag.variations.size
        raise Invalid unless raw["salt"].is_a?(String) && Num.count?(raw["version"])

        flag.salt = raw["salt"]
        flag.version = raw["version"].to_i
        raise Invalid unless raw["targets"].is_a?(Array) && raw["prerequisites"].is_a?(Array)
        raise Invalid unless raw["rules"].is_a?(Array)

        flag.targets = targets(raw["targets"], count)
        flag.prerequisites = prerequisites(raw["prerequisites"])
        flag.rules = raw["rules"].map { |r| rule(r, count) }
        flag.fallthrough = serve(raw["fallthrough"], count)
      end

      def context_lists(raw)
        return [] if raw.nil?
        raise Invalid unless raw.is_a?(Array)

        raw.map do |item|
          raise Invalid unless object?(item) && Context.kind?(item["contextKind"])
          raise Invalid unless string_list?(item["values"])

          [item["contextKind"], item["values"]]
        end
      end

      def key_sets(users, lists)
        raise Invalid unless string_list?(users)

        sets = { Context::DEFAULT_KIND => users.to_h { |k| [k, true] } }
        context_lists(lists).each do |kind, values|
          set = (sets[kind] ||= {})
          values.each { |k| set[k] = true }
        end
        sets
      end

      def segment_rule(raw)
        raise Invalid unless object?(raw)

        weight = raw["weight"]
        raise Invalid unless weight.nil? || (Num.count?(weight) && weight <= BUCKET_COUNT)

        kind = optional_kind(raw["rolloutContextKind"])
        clauses = raw["clauses"]
        raise Invalid unless clauses.is_a?(Array)

        SegmentRule.new(clauses.map { |c| attribute_clause(c) }, weight&.to_i, kind)
      end

      # The Segment, or nil when it does not fit evaluation.md.
      def segment(raw)
        raise Invalid unless object?(raw) && key?(raw["key"]) && raw["salt"].is_a?(String)
        raise Invalid unless raw["rules"].is_a?(Array)

        Segment.new(raw["key"], raw["salt"], key_sets(raw["included"], raw["includedContexts"]),
                    key_sets(raw["excluded"], raw["excludedContexts"]), raw["rules"].map { |r| segment_rule(r) })
      rescue Invalid
        nil
      end

      # The first bad segmentMatch reference in rule, clause and value order.
      def segment_error(flag, segments)
        flag.rules.each do |rule|
          rule.clauses.each do |clause|
            (clause.segments || []).each do |key|
              return ErrorCode::SEGMENT_NOT_FOUND unless segments.key?(key)
              return ErrorCode::MALFORMED_SEGMENT if segments[key].nil?
            end
          end
        end
        nil
      end

      def string_key(item)
        return nil unless item.is_a?(Hash)

        key = item["key"]
        key.is_a?(String) ? key : nil
      end

      def load(raw)
        flags_raw = raw.is_a?(Hash) ? raw["flags"] : nil
        segments_raw = raw.is_a?(Hash) ? raw["segments"] : nil
        segments = {}
        if segments_raw.is_a?(Array)
          segments_raw.each do |item|
            key = string_key(item)
            segments[key] = segment(item) if key && !segments.key?(key)
          end
        end
        flags = {}
        if flags_raw.is_a?(Array)
          flags_raw.each do |item|
            key = string_key(item)
            next if key.nil? || flags.key?(key)

            flags[key] = checked(flag(item), segments)
          end
        end
        Ruleset.new(flags, segments)
      end

      def checked(entry, segments)
        return entry unless entry.status == "ready" && entry.flag

        code = segment_error(entry.flag, segments)
        code.nil? ? entry : Entry.new("broken", entry.flag, code)
      end
    end
  end
end
