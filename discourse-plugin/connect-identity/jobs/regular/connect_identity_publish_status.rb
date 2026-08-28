# frozen_string_literal: true

module Jobs
  class ConnectIdentityPublishStatus < ::Jobs::Base
    def execute(args)
      user = User.find_by(id: args[:user_id])
      ConnectIdentity::Publisher.publish(user) if user
    end
  end
end
