# frozen_string_literal: true

class ConnectIdentity::LevelProgressSerializer
  SCHEMA_VERSION = 1

  def initialize(user)
    @user = user
  end

  def to_h
    {
      schema_version: SCHEMA_VERSION,
      discourse_id: @user.id,
      current_level: level_info(current_level),
      next_level: next_level_id.nil? ? nil : level_info(next_level_id),
      promotion_mode: promotion_mode,
      requirements_met: requirements_met,
      requirements: requirements,
      blocking_conditions: blocking_conditions,
      generated_at: Time.zone.now.utc.iso8601
    }
  end

  private

  def current_level
    @user.trust_level.to_i
  end

  def next_level_id
    return nil if current_level >= 4

    current_level + 1
  end

  def promotion_mode
    return "none" if current_level >= 4
    return "locked" if @user.manual_locked_trust_level.present?
    return "manual" if next_level_id == 4

    "automatic"
  end

  def requirements_met
    return nil unless promotion_mode == "automatic"

    requirements.all? { |requirement| requirement[:met] } &&
      blocking_conditions.all? { |condition| condition[:met] }
  end

  def requirements
    @requirements ||=
      case next_level_id
      when 1
        tl1_requirements
      when 2
        tl2_requirements
      when 3
        tl3_requirements
      else
        []
      end
  end

  def blocking_conditions
    @blocking_conditions ||= begin
      conditions = []
      if @user.manual_locked_trust_level.present? && current_level < 4
        conditions << blocking_condition(
          key: "not_manually_locked",
          default_label: "Trust level is not manually locked",
          met: false
        )
      end
      conditions.concat(tl3_blocking_conditions) if next_level_id == 3
      conditions
    end
  end

  def tl1_requirements
    [
      lifetime_requirement(
        key: "topics_entered",
        default_label: "Topics entered",
        group: "activity",
        current: user_stat.topics_entered,
        target: SiteSetting.tl1_requires_topics_entered
      ),
      lifetime_requirement(
        key: "posts_read",
        default_label: "Posts read",
        group: "activity",
        current: user_stat.posts_read_count,
        target: SiteSetting.tl1_requires_read_posts
      ),
      lifetime_requirement(
        key: "time_read_minutes",
        default_label: "Time read",
        group: "activity",
        current: user_stat.time_read.to_i / 60,
        target: SiteSetting.tl1_requires_time_spent_mins,
        unit: "minutes"
      ),
      lifetime_requirement(
        key: "account_age_minutes",
        default_label: "Account age",
        group: "activity",
        current: account_age_minutes,
        target: SiteSetting.tl1_requires_time_spent_mins,
        unit: "minutes"
      )
    ]
  end

  def tl2_requirements
    tl1 = [
      lifetime_requirement(
        key: "topics_entered",
        default_label: "Topics entered",
        group: "activity",
        current: user_stat.topics_entered,
        target: SiteSetting.tl2_requires_topics_entered
      ),
      lifetime_requirement(
        key: "posts_read",
        default_label: "Posts read",
        group: "activity",
        current: user_stat.posts_read_count,
        target: SiteSetting.tl2_requires_read_posts
      ),
      lifetime_requirement(
        key: "time_read_minutes",
        default_label: "Time read",
        group: "activity",
        current: user_stat.time_read.to_i / 60,
        target: SiteSetting.tl2_requires_time_spent_mins,
        unit: "minutes"
      ),
      lifetime_requirement(
        key: "account_age_minutes",
        default_label: "Account age",
        group: "activity",
        current: account_age_minutes,
        target: SiteSetting.tl2_requires_time_spent_mins,
        unit: "minutes"
      ),
      lifetime_requirement(
        key: "days_visited",
        default_label: "Days visited",
        group: "activity",
        current: user_stat.days_visited,
        target: SiteSetting.tl2_requires_days_visited,
        unit: "days"
      )
    ]

    tl1 + [
      lifetime_requirement(
        key: "likes_received",
        default_label: "Likes received",
        group: "interaction",
        current: user_stat.likes_received,
        target: SiteSetting.tl2_requires_likes_received
      ),
      lifetime_requirement(
        key: "likes_given",
        default_label: "Likes given",
        group: "interaction",
        current: user_stat.likes_given,
        target: SiteSetting.tl2_requires_likes_given
      ),
      lifetime_requirement(
        key: "num_topics_replied_to",
        default_label: "Topics replied to",
        group: "interaction",
        current: user_stat.calc_topic_reply_count!,
        target: SiteSetting.tl2_requires_topic_reply_count
      )
    ]
  end

  def tl3_requirements
    period_days = tl3_rule.time_period

    [
      rolling_requirement(
        key: "days_visited",
        default_label: "Days visited",
        group: "activity",
        current: tl3_rule.days_visited,
        target: tl3_rule.min_days_visited,
        unit: "days",
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_topics_replied_to",
        default_label: "Topics replied to",
        group: "interaction",
        current: tl3_rule.num_topics_replied_to,
        target: tl3_rule.min_topics_replied_to,
        period_days: period_days
      ),
      rolling_requirement(
        key: "topics_viewed",
        default_label: "Topics viewed",
        group: "activity",
        current: tl3_rule.topics_viewed,
        target: tl3_rule.min_topics_viewed,
        period_days: period_days
      ),
      rolling_requirement(
        key: "posts_read",
        default_label: "Posts read",
        group: "activity",
        current: tl3_rule.posts_read,
        target: tl3_rule.min_posts_read,
        period_days: period_days
      ),
      all_time_requirement(
        key: "topics_viewed_all_time",
        default_label: "Topics viewed all time",
        group: "activity",
        current: tl3_rule.topics_viewed_all_time,
        target: tl3_rule.min_topics_viewed_all_time
      ),
      all_time_requirement(
        key: "posts_read_all_time",
        default_label: "Posts read all time",
        group: "activity",
        current: tl3_rule.posts_read_all_time,
        target: tl3_rule.min_posts_read_all_time
      ),
      rolling_requirement(
        key: "num_likes_given",
        default_label: "Likes given",
        group: "interaction",
        current: tl3_rule.num_likes_given,
        target: tl3_rule.min_likes_given,
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_likes_received",
        default_label: "Likes received",
        group: "interaction",
        current: tl3_rule.num_likes_received,
        target: tl3_rule.min_likes_received,
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_likes_received_days",
        default_label: "Days with likes received",
        group: "interaction",
        current: tl3_rule.num_likes_received_days,
        target: tl3_rule.min_likes_received_days,
        unit: "days",
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_likes_received_users",
        default_label: "Users who liked your posts",
        group: "interaction",
        current: tl3_rule.num_likes_received_users,
        target: tl3_rule.min_likes_received_users,
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_flagged_posts",
        default_label: "Flagged posts",
        group: "compliance",
        current: tl3_rule.num_flagged_posts,
        target: tl3_rule.max_flagged_posts,
        operator: "at_most",
        period_days: period_days
      ),
      rolling_requirement(
        key: "num_flagged_by_users",
        default_label: "Users who flagged your posts",
        group: "compliance",
        current: tl3_rule.num_flagged_by_users,
        target: tl3_rule.max_flagged_by_users,
        operator: "at_most",
        period_days: period_days
      )
    ]
  end

  def tl3_blocking_conditions
    [
      blocking_condition(
        key: "not_silenced",
        default_label: "Account is not silenced",
        met: !@user.silenced?
      ),
      blocking_condition(
        key: "not_suspended",
        default_label: "Account is not suspended",
        met: !@user.suspended?
      ),
      blocking_condition(
        key: "no_recent_penalties",
        default_label: "No recent penalties",
        met: tl3_rule.penalty_counts.total == 0
      )
    ]
  end

  def lifetime_requirement(key:, default_label:, group:, current:, target:, unit: "count")
    requirement(
      key: key,
      default_label: default_label,
      group: group,
      scope: "account_lifetime",
      period_days: nil,
      current: current,
      target: target,
      operator: "at_least",
      unit: unit
    )
  end

  def rolling_requirement(key:, default_label:, group:, current:, target:, period_days:, operator: "at_least", unit: "count")
    requirement(
      key: key,
      default_label: default_label,
      group: group,
      scope: "rolling_period",
      period_days: period_days,
      current: current,
      target: target,
      operator: operator,
      unit: unit
    )
  end

  def all_time_requirement(key:, default_label:, group:, current:, target:, operator: "at_least", unit: "count")
    requirement(
      key: key,
      default_label: default_label,
      group: group,
      scope: "all_time",
      period_days: nil,
      current: current,
      target: target,
      operator: operator,
      unit: unit
    )
  end

  def requirement(key:, default_label:, group:, scope:, period_days:, current:, target:, operator:, unit:)
    current_value = current.to_i
    target_value = target.to_i

    {
      key: key,
      label: translate_requirement(key, default_label),
      group: group,
      scope: scope,
      period_days: period_days,
      current: current_value,
      target: target_value,
      operator: operator,
      unit: unit,
      met: compare(current_value, target_value, operator)
    }
  end

  def blocking_condition(key:, default_label:, met:)
    {
      key: key,
      label: translate_blocking_condition(key, default_label),
      met: !!met
    }
  end

  def compare(current, target, operator)
    case operator
    when "at_most"
      current <= target
    when "equals"
      current == target
    else
      current >= target
    end
  end

  def level_info(level)
    key = TrustLevel.levels[level].to_s
    {
      id: level,
      key: key,
      label: I18n.t("js.trust_levels.names.#{key}", default: default_trust_level_label(level))
    }
  end

  def translate_requirement(key, default_label)
    I18n.t("connect_identity.level_progress.requirements.#{key}", default: default_label)
  end

  def translate_blocking_condition(key, default_label)
    I18n.t("connect_identity.level_progress.blocking_conditions.#{key}", default: default_label)
  end

  def default_trust_level_label(level)
    {
      0 => "New User",
      1 => "Basic",
      2 => "Member",
      3 => "Regular",
      4 => "Leader"
    }.fetch(level, "Unknown")
  end

  def account_age_minutes
    ((Time.zone.now - @user.created_at) / 60).floor
  end

  def user_stat
    @user_stat ||= @user.user_stat
  end

  def tl3_rule
    @tl3_rule ||= TrustLevel3Requirements.new(@user)
  end
end
